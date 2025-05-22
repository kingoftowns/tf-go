package constants

// DefaultEnvironment is the default environment used throughout the application
const DefaultEnvironment = "usgw1-dev-devops"

// DefaultTerraformAction is the default action when none is specified
const DefaultTerraformAction = "plan"

// DefaultVaultAddress is the default Vault server address
const DefaultVaultAddress = "http://127.0.0.1:8200"

// DefaultVaultAuthMethod is the default authentication method for Vault
const DefaultVaultAuthMethod = "token"

// DefaultTerraformBackendType is the default backend type for Terraform
const DefaultTerraformBackendType = "local"

// DefaultVarsPathTemplate is the default template for tfvars file paths
const DefaultVarsPathTemplate = "./tfvars/{{env}}/{{stack}}.tfvars"

// DefaultStackPathTemplate is the default template for stack paths  
const DefaultStackPathTemplate = "./app/stacks/{{stack}}"

// DefaultProviderPathTemplate is the default template for provider paths in Vault
const DefaultProviderPathTemplate = "terraform/data/providers"