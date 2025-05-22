package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// S3BackendConfig represents configuration for an S3 backend
type S3BackendConfig struct {
	Bucket         string
	Key            string
	Region         string
	DynamoDBTable  string
	Encrypt        bool
	RoleARN        string
	Profile        string
}

// EnsureS3Backend creates the S3 bucket and DynamoDB table if they don't exist
func EnsureS3Backend(ctx context.Context, cfg S3BackendConfig) error {
	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	s3Client := s3.NewFromConfig(awsCfg)
	
	// Check if bucket exists
	_, err = s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	
	if err != nil {
		// Bucket doesn't exist, create it
		_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(cfg.Bucket),
			// Add location constraint if not in us-east-1
			CreateBucketConfiguration: getS3BucketConfiguration(cfg.Region),
		})
		
		if err != nil {
			return fmt.Errorf("failed to create S3 bucket: %w", err)
		}
		
		// Enable versioning on the bucket
		_, err = s3Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(cfg.Bucket),
			VersioningConfiguration: &s3types.VersioningConfiguration{
				Status: s3types.BucketVersioningStatusEnabled,
			},
		})
		
		if err != nil {
			return fmt.Errorf("failed to enable versioning on S3 bucket: %w", err)
		}
		
		// Enable default encryption if required
		if cfg.Encrypt {
			_, err = s3Client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
				Bucket: aws.String(cfg.Bucket),
				ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
					Rules: []s3types.ServerSideEncryptionRule{
						{
							ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
								SSEAlgorithm: s3types.ServerSideEncryptionAes256,
							},
							BucketKeyEnabled: aws.Bool(true),
						},
					},
				},
			})
			
			if err != nil {
				return fmt.Errorf("failed to configure encryption on S3 bucket: %w", err)
			}
		}
	}
	
	// If DynamoDB locking is required
	if cfg.DynamoDBTable != "" {
		// Create DynamoDB client
		dynamoClient := dynamodb.NewFromConfig(awsCfg)
		
		// Check if table exists
		_, err = dynamoClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(cfg.DynamoDBTable),
		})
		
		if err != nil {
			// Table doesn't exist, create it
			_, err = dynamoClient.CreateTable(ctx, &dynamodb.CreateTableInput{
				TableName: aws.String(cfg.DynamoDBTable),
				AttributeDefinitions: []dynamodbtypes.AttributeDefinition{
					{
						AttributeName: aws.String("LockID"),
						AttributeType: dynamodbtypes.ScalarAttributeTypeS,
					},
				},
				KeySchema: []dynamodbtypes.KeySchemaElement{
					{
						AttributeName: aws.String("LockID"),
						KeyType:       dynamodbtypes.KeyTypeHash,
					},
				},
				BillingMode: dynamodbtypes.BillingModePayPerRequest,
			})
			
			if err != nil {
				return fmt.Errorf("failed to create DynamoDB table: %w", err)
			}
		}
	}
	
	return nil
}

// getS3BucketConfiguration returns the bucket configuration for the specified region
func getS3BucketConfiguration(region string) *s3types.CreateBucketConfiguration {
	// Only needed for regions other than us-east-1
	if region == "us-east-1" {
		return nil
	}
	
	return &s3types.CreateBucketConfiguration{
		LocationConstraint: s3types.BucketLocationConstraint(region),
	}
}

// GenerateBackendConfig creates a Terraform backend configuration
func GenerateBackendConfig(cfg S3BackendConfig) map[string]interface{} {
	backendConfig := map[string]interface{}{
		"bucket": cfg.Bucket,
		"key":    cfg.Key,
		"region": cfg.Region,
	}
	
	if cfg.DynamoDBTable != "" {
		backendConfig["dynamodb_table"] = cfg.DynamoDBTable
	}
	
	if cfg.Encrypt {
		backendConfig["encrypt"] = true
	}
	
	if cfg.RoleARN != "" {
		backendConfig["role_arn"] = cfg.RoleARN
	}
	
	return backendConfig
}

// CreateBackendFile generates a backend.tf file for Terraform
func (e *Executor) CreateBackendFile(cfg S3BackendConfig) error {
	// Check if AWS_PROFILE is set and use it in the backend config
	profile := os.Getenv("AWS_PROFILE")
	
	backendContent := `
terraform {
  backend "s3" {
    bucket         = "{{.Bucket}}"
    key            = "{{.Key}}"
    region         = "{{.Region}}"
    {{- if .DynamoDBTable }}
    dynamodb_table = "{{.DynamoDBTable}}"
    {{- end }}
    {{- if .Encrypt }}
    encrypt        = true
    {{- end }}
    {{- if .RoleARN }}
    role_arn       = "{{.RoleARN}}"
    {{- end }}
    {{- if .Profile }}
    profile        = "{{.Profile}}"
    {{- end }}
  }
}
`
	
	// Add profile to config if set
	if profile != "" {
		cfg.Profile = profile
	}
	
	tmpl, err := template.New("backend").Parse(backendContent)
	if err != nil {
		return err
	}
	
	backendFile := filepath.Join(e.workDir, "backend.tf")
	f, err := os.Create(backendFile)
	if err != nil {
		return err
	}
	defer f.Close()
	
	return tmpl.Execute(f, cfg)
}

// ResolveS3BackendConfig processes environment variables and templates in S3 backend config
func ResolveS3BackendConfig(ctx context.Context, input S3BackendConfig, env, stack string) S3BackendConfig {
	// Create a copy of the input config
	result := input
	
	// Get AWS account ID for bucket naming
	accountID := os.Getenv("AWS_ACCOUNT_ID")
	fmt.Printf("[DEBUG] AWS_ACCOUNT_ID env var: %s\n", accountID)
	
	if accountID == "" {
		// Try to get account ID from AWS profile if available
		profile := os.Getenv("AWS_PROFILE")
		fmt.Printf("[DEBUG] AWS_PROFILE env var: %s\n", profile)
		
		if profile != "" {
			fmt.Printf("[DEBUG] Attempting to load AWS config with profile: %s\n", profile)
			if awsCfg, err := config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile)); err == nil {
				fmt.Printf("[DEBUG] Successfully loaded AWS config\n")
				stsClient := sts.NewFromConfig(awsCfg)
				if identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err == nil && identity.Account != nil {
					accountID = *identity.Account
					fmt.Printf("[DEBUG] Got account ID from STS: %s\n", accountID)
				} else {
					fmt.Printf("[DEBUG] Failed to get caller identity: %v\n", err)
				}
			} else {
				fmt.Printf("[DEBUG] Failed to load AWS config: %v\n", err)
			}
		}
		// Fallback if we still can't get it
		if accountID == "" {
			fmt.Printf("[DEBUG] Using fallback account ID: ACCOUNT\n")
			accountID = "ACCOUNT"
		}
	}
	
	// If input config is empty, use the backend.rb equivalent logic
	if result.Bucket == "" {
		result.Bucket = fmt.Sprintf("terraform-state-k8s-cluster-info-%s", accountID)
	}
	
	if result.Key == "" {
		result.Key = fmt.Sprintf("%s/%s/terraform.tfstate", env, stack)
	}
	
	if result.DynamoDBTable == "" {
		result.DynamoDBTable = "terraform_locks_k8s-cluster-info"
	}
	
	if result.Region == "" {
		result.Region = "us-gov-west-1" // Default region from your backend.rb
	}
	
	result.Encrypt = true // Always encrypt as per backend.rb
	
	// Process string templating for any remaining placeholders
	result.Bucket = strings.ReplaceAll(result.Bucket, ":ENV", env)
	result.Bucket = strings.ReplaceAll(result.Bucket, ":STACK", stack)
	result.Bucket = strings.ReplaceAll(result.Bucket, ":ACCOUNT", accountID)
	
	result.Key = strings.ReplaceAll(result.Key, ":ENV", env)
	result.Key = strings.ReplaceAll(result.Key, ":STACK", stack)
	result.Key = strings.ReplaceAll(result.Key, ":MOD_NAME", stack)
	
	if result.DynamoDBTable != "" {
		result.DynamoDBTable = strings.ReplaceAll(result.DynamoDBTable, ":ENV", env)
		result.DynamoDBTable = strings.ReplaceAll(result.DynamoDBTable, ":STACK", stack)
	}
	
	return result
}