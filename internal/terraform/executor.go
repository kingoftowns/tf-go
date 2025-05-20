package terraform

import (
	"context"
	"fmt"
	"io"
	"os"
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

// Setup prepares the Terraform workspace
func (e *Executor) Setup(ctx context.Context, srcPath string, providerConfig map[string]interface{}) error {
	// Copy source files
	err := copyDir(srcPath, e.workDir)
	if err != nil {
		return fmt.Errorf("failed to copy Terraform files: %w", err)
	}

	// Create provider.tf file
	err = e.createProviderFile(providerConfig)
	if err != nil {
		return fmt.Errorf("failed to create provider file: %w", err)
	}

	// Find Terraform executable
	tfPath := "terraform" // Use the terraform in PATH
	// if you need to find it programmatically, you'll need a custom function

	// Create Terraform executor
	e.tf, err = tfexec.NewTerraform(e.workDir, tfPath)
	if err != nil {
		return fmt.Errorf("failed to create Terraform executor: %w", err)
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

// createProviderFile generates provider.tf in the working directory
func (e *Executor) createProviderFile(providerConfig map[string]interface{}) error {
	// This is a simplified provider template generator
	// In a real implementation, this would be more dynamic and support multiple providers

	// AWS provider example
	if awsConfig, ok := providerConfig["aws"].(map[string]interface{}); ok {
		providerTpl := `
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
}
`
		tmpl, err := template.New("provider").Parse(providerTpl)
		if err != nil {
			return err
		}

		f, err := os.Create(filepath.Join(e.workDir, "provider.tf"))
		if err != nil {
			return err
		}
		defer f.Close()

		return tmpl.Execute(f, awsConfig)
	}

	// Add templates for other providers (Azure, GCP, etc.) as needed

	return fmt.Errorf("no supported provider configuration found")
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
