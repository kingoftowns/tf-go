package terraform

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// Executor handles Terraform operations
type Executor struct {
	workDir    string
	tf         *tfexec.Terraform
	envVars    map[string]string
	cleanupFns []func() error
}

// NewExecutor creates a new Terraform executor
func NewExecutor(ctx context.Context) (*Executor, error) {
	// Create a temporary working directory
	workDir, err := os.MkdirTemp("", "terraform-deployer-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// Create executor
	executor := &Executor{
		workDir: workDir,
		envVars: make(map[string]string),
	}

	// Add cleanup function
	executor.cleanupFns = append(executor.cleanupFns, func() error {
		return os.RemoveAll(workDir)
	})

	return executor, nil
}

// Clean removes the temporary working directory and performs other cleanup
func (e *Executor) Clean() error {
	var errs []string
	for _, fn := range e.cleanupFns {
		if err := fn(); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ResolveEnvVars processes a map and resolves any environment variable references
func ResolveEnvVars(input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	for key, value := range input {
		switch v := value.(type) {
		case string:
			// Handle string environment variables using ${ENV:VAR_NAME} syntax
			if len(v) > 7 && v[:6] == "${ENV:" && v[len(v)-1:] == "}" {
				envVar := v[6 : len(v)-1]
				if envValue := os.Getenv(envVar); envValue != "" {
					result[key] = envValue
				} else {
					// Keep the original if env var not found
					result[key] = v
				}
			} else {
				result[key] = v
			}
		case map[string]interface{}:
			// Recursively resolve nested maps
			result[key] = ResolveEnvVars(v)
		case []interface{}:
			// Handle array values
			resolvedArray := make([]interface{}, len(v))
			for i, item := range v {
				if nestedMap, ok := item.(map[string]interface{}); ok {
					resolvedArray[i] = ResolveEnvVars(nestedMap)
				} else {
					resolvedArray[i] = item
				}
			}
			result[key] = resolvedArray
		default:
			result[key] = v
		}
	}
	
	return result
}

// getEKSClusterEndpoint retrieves the endpoint for an EKS cluster
func getEKSClusterEndpoint(ctx context.Context, clusterName string) (string, error) {
	// In a real implementation, this would use AWS SDK to get the cluster endpoint
	// Example with AWS SDK (pseudocode):
	// 
	// import (
	//     "github.com/aws/aws-sdk-go-v2/aws"
	//     "github.com/aws/aws-sdk-go-v2/service/eks"
	// )
	//
	// cfg, err := aws.LoadDefaultConfig(ctx)
	// if err != nil {
	//     return "", err
	// }
	//
	// client := eks.NewFromConfig(cfg)
	// result, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
	//     Name: aws.String(clusterName),
	// })
	// if err != nil {
	//     return "", err
	// }
	//
	// return *result.Cluster.Endpoint, nil

	// For demonstration, we'll execute the AWS CLI command
	// This is not ideal for production; the AWS SDK should be used instead
	cmd := fmt.Sprintf("aws eks describe-cluster --name %s --query 'cluster.endpoint' --output text", clusterName)
	out, err := executeCommand(ctx, cmd)
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(out), nil
}

// getEKSClusterCA retrieves the CA certificate for an EKS cluster
func getEKSClusterCA(ctx context.Context, clusterName string) (string, error) {
	// In a real implementation, this would use AWS SDK to get the cluster CA certificate
	
	// For demonstration, we'll execute the AWS CLI command
	cmd := fmt.Sprintf("aws eks describe-cluster --name %s --query 'cluster.certificateAuthority.data' --output text", clusterName)
	out, err := executeCommand(ctx, cmd)
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(out), nil
}

// getEKSClusterToken generates a token for authenticating with an EKS cluster
func getEKSClusterToken(ctx context.Context, clusterName string) (string, error) {
	// In a real implementation, this would use AWS SDK to generate a token
	
	// For demonstration, we'll execute the AWS CLI command
	cmd := fmt.Sprintf("aws eks get-token --cluster-name %s --query 'status.token' --output text", clusterName)
	out, err := executeCommand(ctx, cmd)
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(out), nil
}

// executeCommand runs a command and returns its output
func executeCommand(ctx context.Context, cmd string) (string, error) {
	// This is a basic implementation; in production, you would want more robust handling
	parts := strings.Fields(cmd)
	
	command := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	
	return string(output), nil
}

// ResolveDynamicValues processes dynamic values that need to be computed
func ResolveDynamicValues(ctx context.Context, input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	for key, value := range input {
		switch v := value.(type) {
		case string:
			// Handle dynamic values using ${DYNAMIC:VALUE_TYPE} syntax
			if len(v) > 11 && v[:10] == "${DYNAMIC:" && v[len(v)-1:] == "}" {
				dynamicType := v[10 : len(v)-1]
				
				// Handle different types of dynamic values
				switch dynamicType {
				case "EKS_CLUSTER_ENDPOINT":
					// Get EKS cluster endpoint
					if clusterName := os.Getenv("CLUSTER_NAME"); clusterName != "" {
						endpoint, err := getEKSClusterEndpoint(ctx, clusterName)
						if err == nil {
							result[key] = endpoint
						} else {
							result[key] = v // Keep original on error
						}
					} else {
						result[key] = v
					}
				case "EKS_CLUSTER_TOKEN":
					// Get EKS cluster token
					if clusterName := os.Getenv("CLUSTER_NAME"); clusterName != "" {
						token, err := getEKSClusterToken(ctx, clusterName)
						if err == nil {
							result[key] = token
						} else {
							result[key] = v // Keep original on error
						}
					} else {
						result[key] = v
					}
				case "EKS_CLUSTER_CA":
					// Get EKS cluster CA certificate
					if clusterName := os.Getenv("CLUSTER_NAME"); clusterName != "" {
						ca, err := getEKSClusterCA(ctx, clusterName)
						if err == nil {
							// Base64 decode for Terraform provider
							decoded, err := base64.StdEncoding.DecodeString(ca)
							if err == nil {
								result[key] = string(decoded)
							} else {
								result[key] = v // Keep original on error
							}
						} else {
							result[key] = v // Keep original on error
						}
					} else {
						result[key] = v
					}
				default:
					result[key] = v
				}
			} else {
				result[key] = v
			}
		case map[string]interface{}:
			// Recursively resolve nested maps
			result[key] = ResolveDynamicValues(ctx, v)
		case []interface{}:
			// Handle array values
			resolvedArray := make([]interface{}, len(v))
			for i, item := range v {
				if nestedMap, ok := item.(map[string]interface{}); ok {
					resolvedArray[i] = ResolveDynamicValues(ctx, nestedMap)
				} else {
					resolvedArray[i] = item
				}
			}
			result[key] = resolvedArray
		default:
			result[key] = v
		}
	}
	
	return result
}

// Setup prepares the Terraform workspace
func (e *Executor) Setup(ctx context.Context, srcPath string, providerConfig map[string]interface{}, backendConfig *S3BackendConfig) error {
	// Copy source files
	err := copyDir(srcPath, e.workDir)
	if err != nil {
		return fmt.Errorf("failed to copy Terraform files: %w", err)
	}

	// Process provider config to resolve environment variables and dynamic values
	resolvedConfig := providerConfig
	resolvedConfig = ResolveEnvVars(resolvedConfig)
	resolvedConfig = ResolveDynamicValues(ctx, resolvedConfig)

	// Create provider.tf file
	err = e.createProviderFile(resolvedConfig)
	if err != nil {
		return fmt.Errorf("failed to create provider file: %w", err)
	}

	// Setup backend if provided
	if backendConfig != nil {
		// Ensure S3 bucket and DynamoDB table exist
		if err := EnsureS3Backend(ctx, *backendConfig); err != nil {
			return fmt.Errorf("failed to ensure S3 backend: %w", err)
		}

		// Create backend.tf file
		if err := e.CreateBackendFile(*backendConfig); err != nil {
			return fmt.Errorf("failed to create backend file: %w", err)
		}
	}

	// Find Terraform executable
	tfPath := "terraform" // Use the terraform in PATH
	// if you need to find it programmatically, you'll need a custom function

	// Create Terraform executor
	e.tf, err = tfexec.NewTerraform(e.workDir, tfPath)
	if err != nil {
		return fmt.Errorf("failed to create Terraform executor: %w", err)
	}

	// Transfer environment variables to Terraform
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			e.envVars[parts[0]] = parts[1]
		}
	}

	// Set environment variables
	e.tf.SetEnv(e.envVars)

	return nil
}

// Init initializes Terraform
func (e *Executor) Init(ctx context.Context) error {
	if e.tf == nil {
		return fmt.Errorf("terraform executor not set up")
	}

	// Initialize Terraform
	return e.tf.Init(ctx)
}

// Plan runs terraform plan
func (e *Executor) Plan(ctx context.Context, varsFile string) (*tfjson.Plan, error) {
	if e.tf == nil {
		return nil, fmt.Errorf("terraform executor not set up")
	}

	var opts []tfexec.PlanOption
	if varsFile != "" {
		opts = append(opts, tfexec.VarFile(varsFile))
	}

	// Run plan
	_, err := e.tf.Plan(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("terraform plan failed: %w", err)
	}

	// Return empty plan - in real implementation you would either:
	// 1. Generate a proper plan output
	// 2. Or simply return a text representation instead of a structured object
	return &tfjson.Plan{
		ResourceChanges: []*tfjson.ResourceChange{},
	}, nil
}

// Apply runs terraform apply
func (e *Executor) Apply(ctx context.Context, varsFile string) error {
	if e.tf == nil {
		return fmt.Errorf("terraform executor not set up")
	}

	var opts []tfexec.ApplyOption
	if varsFile != "" {
		opts = append(opts, tfexec.VarFile(varsFile))
	}

	// Run apply
	return e.tf.Apply(ctx, opts...)
}

// Destroy runs terraform destroy
func (e *Executor) Destroy(ctx context.Context, varsFile string) error {
	if e.tf == nil {
		return fmt.Errorf("terraform executor not set up")
	}

	var opts []tfexec.DestroyOption
	if varsFile != "" {
		opts = append(opts, tfexec.VarFile(varsFile))
	}

	// Run destroy
	return e.tf.Destroy(ctx, opts...)
}

// Output gets outputs from terraform
func (e *Executor) Output(ctx context.Context) (map[string]*tfjson.StateOutput, error) {
	if e.tf == nil {
		return nil, fmt.Errorf("terraform executor not set up")
	}

	// Get outputs from Terraform
	outputs, err := e.tf.Output(ctx)
	if err != nil {
		return nil, err
	}
	
	// Convert to StateOutput format
	result := make(map[string]*tfjson.StateOutput)
	for k, v := range outputs {
		result[k] = &tfjson.StateOutput{
			Sensitive: v.Sensitive,
			Value:     v.Value,
		}
	}
	
	return result, nil
}

// SetEnvVar sets an environment variable for Terraform
func (e *Executor) SetEnvVar(key, value string) {
	e.envVars[key] = value
	if e.tf != nil {
		e.tf.SetEnv(e.envVars)
	}
}

// ProviderGenerator interface for creating provider configurations
type ProviderGenerator interface {
	Generate(writer io.Writer, config map[string]interface{}) error
}

// AWSProviderGenerator handles AWS provider configuration
type AWSProviderGenerator struct{}

func (g *AWSProviderGenerator) Generate(writer io.Writer, config map[string]interface{}) error {
	tmpl, err := template.New("aws_provider").Parse(`
provider "aws" {
  region     = "{{ .region }}"
  {{- if .access_key }}
  access_key = "{{ .access_key }}"
  {{- end }}
  {{- if .secret_key }}
  secret_key = "{{ .secret_key }}"
  {{- end }}
  {{- if .profile }}
  profile    = "{{ .profile }}"
  {{- end }}
  {{- if .default_tags }}
  default_tags {
    tags = {
      {{- range $key, $value := .default_tags }}
      {{ $key }} = "{{ $value }}"
      {{- end }}
    }
  }
  {{- end }}
}
`)
	if err != nil {
		return err
	}
	return tmpl.Execute(writer, config)
}

// KubernetesProviderGenerator handles Kubernetes provider configuration
type KubernetesProviderGenerator struct{}

func (g *KubernetesProviderGenerator) Generate(writer io.Writer, config map[string]interface{}) error {
	tmpl, err := template.New("kubernetes_provider").Parse(`
provider "kubernetes" {
  {{- if .config_path }}
  config_path = "{{ .config_path }}"
  {{- end }}
  {{- if .config_context }}
  config_context = "{{ .config_context }}"
  {{- end }}
  {{- if .host }}
  host = "{{ .host }}"
  {{- end }}
  {{- if .token }}
  token = "{{ .token }}"
  {{- end }}
  {{- if .cluster_ca_certificate }}
  cluster_ca_certificate = <<EOT
{{ .cluster_ca_certificate }}
EOT
  {{- end }}
}
`)
	if err != nil {
		return err
	}
	return tmpl.Execute(writer, config)
}

// HelmProviderGenerator handles Helm provider configuration
type HelmProviderGenerator struct{}

func (g *HelmProviderGenerator) Generate(writer io.Writer, config map[string]interface{}) error {
	tmpl, err := template.New("helm_provider").Parse(`
provider "helm" {
  {{- if .kubernetes }}
  kubernetes {
    {{- if .kubernetes.config_path }}
    config_path = "{{ .kubernetes.config_path }}"
    {{- end }}
    {{- if .kubernetes.config_context }}
    config_context = "{{ .kubernetes.config_context }}"
    {{- end }}
    {{- if .kubernetes.host }}
    host = "{{ .kubernetes.host }}"
    {{- end }}
    {{- if .kubernetes.token }}
    token = "{{ .kubernetes.token }}"
    {{- end }}
    {{- if .kubernetes.cluster_ca_certificate }}
    cluster_ca_certificate = <<EOT
{{ .kubernetes.cluster_ca_certificate }}
EOT
    {{- end }}
  }
  {{- end }}
}
`)
	if err != nil {
		return err
	}
	return tmpl.Execute(writer, config)
}

// ProviderGeneratorFactory returns the appropriate generator for a provider type
func ProviderGeneratorFactory(providerType string) (ProviderGenerator, error) {
	switch providerType {
	case "aws":
		return &AWSProviderGenerator{}, nil
	case "kubernetes":
		return &KubernetesProviderGenerator{}, nil
	case "helm":
		return &HelmProviderGenerator{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// checkExistingProviders scans for existing provider blocks in .tf files
func (e *Executor) checkExistingProviders() (map[string]bool, error) {
	existingProviders := make(map[string]bool)
	
	// Get all .tf files in the working directory
	tfFiles, err := filepath.Glob(filepath.Join(e.workDir, "*.tf"))
	if err != nil {
		return nil, err
	}
	
	for _, tfFile := range tfFiles {
		content, err := os.ReadFile(tfFile)
		if err != nil {
			return nil, err
		}
		
		// Simple regex-based detection of provider blocks
		// Look for patterns like: provider "aws" {, provider "kubernetes" {, etc.
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if strings.HasPrefix(trimmedLine, "provider ") && strings.Contains(trimmedLine, "{") {
				// Extract provider name from the line
				// Example: provider "aws" { -> aws
				if start := strings.Index(trimmedLine, "\""); start != -1 {
					end := strings.Index(trimmedLine[start+1:], "\"")
					if end != -1 {
						providerName := trimmedLine[start+1 : start+1+end]
						existingProviders[providerName] = true
					}
				}
			}
		}
	}
	
	return existingProviders, nil
}

// createProviderFile generates provider.tf in the working directory
func (e *Executor) createProviderFile(providerConfig map[string]interface{}) error {
	// Check if any provider files already exist
	existingProviders, err := e.checkExistingProviders()
	if err != nil {
		return fmt.Errorf("failed to check existing providers: %w", err)
	}
	
	// Filter out providers that already exist
	filteredConfig := make(map[string]interface{})
	for providerType, config := range providerConfig {
		if !existingProviders[providerType] {
			filteredConfig[providerType] = config
		}
	}
	
	// If no new providers to add, skip creating the file
	if len(filteredConfig) == 0 {
		return nil
	}
	
	f, err := os.Create(filepath.Join(e.workDir, "provider.tf"))
	if err != nil {
		return err
	}
	defer f.Close()
	
	// Track if we've added any providers
	providersAdded := false
	
	// Process each provider in the filtered configuration
	for providerType, config := range filteredConfig {
		configMap, ok := config.(map[string]interface{})
		if !ok {
			continue
		}
		
		generator, err := ProviderGeneratorFactory(providerType)
		if err != nil {
			// Skip unsupported providers
			continue
		}
		
		if err := generator.Generate(f, configMap); err != nil {
			return fmt.Errorf("failed to generate %s provider: %w", providerType, err)
		}
		
		providersAdded = true
	}
	
	if !providersAdded {
		return fmt.Errorf("no supported provider configuration found")
	}
	
	return nil
}

// Helper to copy directories recursively
func copyDir(src, dst string) error {
	// Get properties of source dir
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("error getting stats for source directory: %w", err)
	}

	// Create the destination directory
	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("error creating destination directory: %w", err)
	}

	items, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("error reading source directory: %w", err)
	}

	for _, item := range items {
		srcPath := filepath.Join(src, item.Name())
		dstPath := filepath.Join(dst, item.Name())

		if item.IsDir() {
			// Recursively copy subdirectories
			if err = copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy files
			if err = copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// Helper to copy a single file
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error opening source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("error creating destination file: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("error copying file contents: %w", err)
	}

	// Get and set permissions from source file
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("error getting stats for source file: %w", err)
	}

	return os.Chmod(dst, srcInfo.Mode())
}
