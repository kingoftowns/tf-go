// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kingoftowns/tf-go/internal/constants"
	"gopkg.in/yaml.v3"
)

// Config represents the global configuration
type Config struct {
	Vault        VaultConfig                  `yaml:"vault"`
	Terraform    TerraformConfig              `yaml:"terraform"`
	Defaults     DefaultsConfig               `yaml:"defaults"`
	Environments map[string]EnvironmentConfig `yaml:"environments,omitempty"`
}

// VaultConfig holds Vault-related configuration
type VaultConfig struct {
	Address    string `yaml:"address"`
	AuthMethod string `yaml:"auth_method"`
	RoleName   string `yaml:"role_name,omitempty"`
	SecretID   string `yaml:"secret_id,omitempty"`
}

// TerraformConfig holds Terraform-related settings
type TerraformConfig struct {
	Version     string `yaml:"version"`
	BackendType string `yaml:"backend_type"`
}

// DefaultsConfig holds default settings
type DefaultsConfig struct {
	Environment          string `yaml:"environment"`
	VarsPathTemplate     string `yaml:"vars_path_template"`
	StackPathTemplate    string `yaml:"stack_path_template"`
	ProviderPathTemplate string `yaml:"provider_path_template"`
}

// EnvironmentConfig represents environment-specific configuration
type EnvironmentConfig struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Vault       EnvVaultConfig         `yaml:"vault"`
	Backend     BackendConfig          `yaml:"backend"`
	Settings    map[string]interface{} `yaml:"settings"`
}

// EnvVaultConfig holds environment-specific Vault configuration
type EnvVaultConfig struct {
	ProviderPath string `yaml:"provider_path"`
}

// BackendConfig holds state backend configuration
type BackendConfig struct {
	Type   string                 `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// LoadConfig loads the global and environment-specific configuration
func LoadConfig(env string) (*Config, error) {
	// Load global config
	cfg := &Config{
		Environments: make(map[string]EnvironmentConfig),
	}

	// Set default configuration
	cfg.Defaults.Environment = constants.DefaultEnvironment
	cfg.Defaults.VarsPathTemplate = constants.DefaultVarsPathTemplate
	cfg.Defaults.StackPathTemplate = constants.DefaultStackPathTemplate
	cfg.Defaults.ProviderPathTemplate = constants.DefaultProviderPathTemplate
	cfg.Terraform.BackendType = constants.DefaultTerraformBackendType
	cfg.Vault.Address = constants.DefaultVaultAddress
	cfg.Vault.AuthMethod = constants.DefaultVaultAuthMethod

	// Look for optional project-local config files
	globalConfigPath := "./config.yaml"
	if _, err := os.Stat(globalConfigPath); err == nil {
		data, err := os.ReadFile(globalConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Load environment-specific config if available
	envConfigPath := filepath.Join("./environments", env+".yaml")
	if _, err := os.Stat(envConfigPath); err == nil {
		data, err := os.ReadFile(envConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read environment config file: %w", err)
		}

		envCfg := EnvironmentConfig{}
		if err := yaml.Unmarshal(data, &envCfg); err != nil {
			return nil, fmt.Errorf("failed to parse environment config file: %w", err)
		}

		cfg.Environments[env] = envCfg
	}

	return cfg, nil
}

// ResolveStackPath resolves the path to a stack
func (c *Config) ResolveStackPath(stack string) string {
	path := c.Defaults.StackPathTemplate
	path = strings.ReplaceAll(path, "{{stack}}", stack)
	return path
}

// ResolveVarsPath resolves the paths to vars files, returning base.tfvars first (if exists) then env-specific tfvars
func (c *Config) ResolveVarsPath(env, stack, terraformPath string) []string {
	var varsPaths []string

	fmt.Printf("[DEBUG] ResolveVarsPath: env=%s, stack=%s, terraformPath=%s\n", env, stack, terraformPath)

	// Look in the tfvars subdirectory first
	tfvarsPath := filepath.Join(terraformPath, "config", "terraform", "tfvars")
	if _, err := os.Stat(tfvarsPath); err == nil {
		fmt.Printf("[DEBUG] Found tfvars directory: %s\n", tfvarsPath)
		terraformPath = tfvarsPath
	}

	// First check for base.tfvars recursively in the terraform directory (excluding hidden dirs)
	fmt.Printf("[DEBUG] Looking for base.tfvars in directory: %s\n", terraformPath)
	err := filepath.Walk(terraformPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Name() == "base.tfvars" {
			fmt.Printf("[DEBUG] Found base.tfvars: %s\n", path)
			varsPaths = append(varsPaths, path)
			return filepath.SkipAll // Stop after first match
		}

		return nil
	})

	if err != nil {
		fmt.Printf("[DEBUG] Error walking directory for base.tfvars: %v\n", err)
	}

	// Find environment-specific tfvars files
	// Look for exact match first: env.tfvars (recursive)
	exactMatchFound := false
	fmt.Printf("[DEBUG] Looking for exact match %s.tfvars\n", env)
	err = filepath.Walk(terraformPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Name() == env+".tfvars" {
			fmt.Printf("[DEBUG] Found exact match: %s\n", path)
			varsPaths = append(varsPaths, path)
			exactMatchFound = true
			return filepath.SkipAll // Stop after first match
		}

		return nil
	})

	if err != nil {
		fmt.Printf("[DEBUG] Error walking directory for exact match: %v\n", err)
	}

	if exactMatchFound {
		fmt.Printf("[DEBUG] Final varsPaths: %v\n", varsPaths)
		return varsPaths
	}

	// If exact match not found, look for files containing the env name (recursive search)
	fmt.Printf("[DEBUG] Looking for files containing env name in directory: %s\n", terraformPath)
	err = filepath.Walk(terraformPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		fileName := info.Name()
		fmt.Printf("[DEBUG] Checking file: %s\n", path)
		if strings.HasSuffix(fileName, ".tfvars") && strings.Contains(fileName, env) {
			fmt.Printf("[DEBUG] Found matching file: %s\n", path)
			varsPaths = append(varsPaths, path)
			return filepath.SkipAll // Stop after first match
		}

		return nil
	})

	if err != nil {
		fmt.Printf("[DEBUG] Error walking directory: %v\n", err)
	}

	fmt.Printf("[DEBUG] Final varsPaths: %v\n", varsPaths)
	return varsPaths
}

// ResolveProviderPath resolves the path to provider config in Vault
func (c *Config) ResolveProviderPath(env string) string {
	// First check if there's an environment-specific provider path
	if envCfg, ok := c.Environments[env]; ok && envCfg.Vault.ProviderPath != "" {
		return envCfg.Vault.ProviderPath
	}

	// Fall back to template
	path := c.Defaults.ProviderPathTemplate
	path = strings.ReplaceAll(path, "{{env}}", env)
	return path
}
