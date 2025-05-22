// cmd/debug/main.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kingoftowns/tf-go/internal/config"
	"github.com/kingoftowns/tf-go/internal/constants"
	"github.com/kingoftowns/tf-go/internal/terraform"
)

func main() {
	ctx := context.Background()

	var (
		envFlag   string
		stackFlag string
	)

	flag.StringVar(&envFlag, "env", constants.DefaultEnvironment, "Environment name")
	flag.StringVar(&stackFlag, "stack", "", "Stack name")
	flag.Parse()

	if stackFlag == "" {
		fmt.Println("Error: --stack flag is required")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("=== Backend Configuration Debug ===\n")
	fmt.Printf("Environment: %s\n", envFlag)
	fmt.Printf("Stack: %s\n", stackFlag)
	fmt.Printf("AWS_PROFILE: %s\n", os.Getenv("AWS_PROFILE"))
	fmt.Printf("AWS_ACCOUNT_ID: %s\n", os.Getenv("AWS_ACCOUNT_ID"))
	fmt.Printf("TF_PATH: %s\n", os.Getenv("TF_PATH"))
	fmt.Printf("VAULT_ADDR: %s\n", os.Getenv("VAULT_ADDR"))
	fmt.Printf("\n")

	// Load config
	cfg, err := config.LoadConfig(envFlag)
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Show resolved backend config
	s3Config := terraform.S3BackendConfig{}

	// Check if environment config overrides S3 settings
	if envConfig, ok := cfg.Environments[envFlag]; ok && envConfig.Backend.Type == "s3" {
		if bucket, ok := envConfig.Backend.Config["bucket"]; ok {
			s3Config.Bucket = fmt.Sprintf("%v", bucket)
		}
		if key, ok := envConfig.Backend.Config["key"]; ok {
			s3Config.Key = fmt.Sprintf("%v", key)
		}
		if region, ok := envConfig.Backend.Config["region"]; ok {
			s3Config.Region = fmt.Sprintf("%v", region)
		}
		if dynamo, ok := envConfig.Backend.Config["dynamodb_table"]; ok {
			s3Config.DynamoDBTable = fmt.Sprintf("%v", dynamo)
		}
	}

	// Apply backend.rb equivalent defaults and resolve placeholders
	resolvedConfig := terraform.ResolveS3BackendConfig(ctx, s3Config, envFlag, stackFlag)

	fmt.Printf("=== Resolved S3 Backend Configuration ===\n")
	configJSON, _ := json.MarshalIndent(resolvedConfig, "", "  ")
	fmt.Printf("%s\n\n", configJSON)

	// Show what the backend.tf file would contain
	fmt.Printf("=== Generated backend.tf Content ===\n")

	// Create a temporary executor to generate the backend file content
	executor, err := terraform.NewExecutor(ctx)
	if err != nil {
		fmt.Printf("Error creating executor: %v\n", err)
		os.Exit(1)
	}
	defer executor.Clean()

	err = executor.CreateBackendFile(resolvedConfig)
	if err != nil {
		fmt.Printf("Error creating backend file: %v\n", err)
		os.Exit(1)
	}

	// Read and display the backend file content
	backendContent, err := os.ReadFile(executor.GetWorkDir() + "/backend.tf")
	if err != nil {
		fmt.Printf("Error reading backend file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s\n", backendContent)

	// Show provider path resolution
	fmt.Printf("=== Provider Configuration ===\n")
	providerPath := cfg.ResolveProviderPath(envFlag)
	fmt.Printf("Provider path in Vault: %s\n", providerPath)

	// Show vars file resolution
	fmt.Printf("\n=== Vars File Resolution ===\n")
	basePath := os.Getenv("TF_PATH")
	if basePath == "" {
		basePath = "."
	}

	terraformPath := basePath
	if stackFlag != "" {
		stackPath := cfg.ResolveStackPath(stackFlag)
		terraformPath = fmt.Sprintf("%s/%s", basePath, stackPath)
	}

	varsFilePaths := cfg.ResolveVarsPath(envFlag, stackFlag, terraformPath)
	fmt.Printf("Terraform path: %s\n", terraformPath)
	fmt.Printf("Vars file paths:\n")
	for i, path := range varsFilePaths {
		exists := "✗"
		if _, err := os.Stat(path); err == nil {
			exists = "✓"
		}
		fmt.Printf("  [%d] %s %s\n", i+1, exists, path)
	}

	// Check if we're in GitLab environment
	fmt.Printf("\n=== Environment Detection ===\n")
	if os.Getenv("GITLAB_CI") == "true" {
		fmt.Printf("Running in GitLab CI: ✓\n")
		fmt.Printf("CI_PROJECT_NAME: %s\n", os.Getenv("CI_PROJECT_NAME"))
		fmt.Printf("CI_COMMIT_REF_NAME: %s\n", os.Getenv("CI_COMMIT_REF_NAME"))
		fmt.Printf("CI_PIPELINE_ID: %s\n", os.Getenv("CI_PIPELINE_ID"))
	} else {
		fmt.Printf("Running locally: ✓\n")
	}

	// Show state file information if accessible
	fmt.Printf("\n=== State File Analysis ===\n")
	stateKey := resolvedConfig.Key
	stateBucket := resolvedConfig.Bucket

	fmt.Printf("Expected state location: s3://%s/%s\n", stateBucket, stateKey)

	// Try to check if state file exists (requires AWS credentials)
	fmt.Printf("\nTo check if the state file exists, run:\n")
	fmt.Printf("  aws s3 ls s3://%s/%s\n", stateBucket, stateKey)
	fmt.Printf("\nTo download and inspect the state file, run:\n")
	fmt.Printf("  aws s3 cp s3://%s/%s ./terraform.tfstate\n", stateBucket, stateKey)
	fmt.Printf("  terraform show -json ./terraform.tfstate | jq '.values.root_module.resources[] | .address'\n")

	// Show potential Terraspace vs tf-go differences
	fmt.Printf("\n=== Potential Terraspace Differences ===\n")
	fmt.Printf("Terraspace typically uses state keys like:\n")
	fmt.Printf("  - :ENV/:STACK/terraform.tfstate\n")
	fmt.Printf("  - :ENV/:MOD_NAME/terraform.tfstate\n")
	fmt.Printf("  - stacks/:STACK/:ENV/terraform.tfstate\n")
	fmt.Printf("\nYour tf-go uses: %s\n", stateKey)

	// Show commands to compare Terraspace vs tf-go state
	fmt.Printf("\n=== Debugging Commands ===\n")
	fmt.Printf("To see what Terraspace is using, check your Terraspace config:\n")
	fmt.Printf("  grep -r \"bucket\\|key\" config/terraform/backend.rb\n")
	fmt.Printf("  terraspace info %s --env %s\n", stackFlag, envFlag)
	fmt.Printf("\nTo compare state files:\n")
	fmt.Printf("  # Download Terraspace state\n")
	fmt.Printf("  terraspace state pull %s --env %s\n", stackFlag, envFlag)
	fmt.Printf("  # Download tf-go state  \n")
	fmt.Printf("  aws s3 cp s3://%s/%s ./tf-go-state.json\n", stateBucket, stateKey)
	fmt.Printf("  # Compare\n")
	fmt.Printf("  diff terraform.tfstate tf-go-state.json\n")

	fmt.Printf("\nIf Terraspace is using a different pattern, this could cause state drift.\n")
}
