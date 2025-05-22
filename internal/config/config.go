// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	cfg.Defaults.Environment = "dev-devops"
	cfg.Defaults.VarsPathTemplate = "./tfvars/{{env}}/{{stack}}.tfvars"
	cfg.Defaults.StackPathTemplate = "./app/stacks/{{stack}}"
	cfg.Defaults.ProviderPathTemplate = "terraform/data/providers"
	cfg.Terraform.BackendType = "local"
	cfg.Vault.Address = "http://127.0.0.1:8200"
	cfg.Vault.AuthMethod = "token"

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

// ResolveVarsPath resolves the path to a vars file
func (c *Config) ResolveVarsPath(env, stack string) string {
	path := c.Defaults.VarsPathTemplate
	path = strings.ReplaceAll(path, "{{env}}", env)
	path = strings.ReplaceAll(path, "{{stack}}", stack)
	return path
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
