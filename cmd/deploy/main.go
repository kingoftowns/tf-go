// cmd/deploy/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kingoftowns/tf-go/internal/config"
	"github.com/kingoftowns/tf-go/internal/constants"
	"github.com/kingoftowns/tf-go/internal/terraform"
	"github.com/kingoftowns/tf-go/internal/vault"
)

func main() {
	ctx := context.Background()

	defaultPath := os.Getenv("TF_PATH")
	defaultEnv := os.Getenv("TF_ENV")
	if defaultEnv == "" {
		defaultEnv = constants.DefaultEnvironment
	}
	defaultAction := os.Getenv("TF_ACTION")
	if defaultAction == "" {
		defaultAction = constants.DefaultTerraformAction
	}
	defaultVaultAddr := os.Getenv("VAULT_ADDR")

	var (
		pathFlag      string
		stackFlag     string
		envFlag       string
		varsFileFlag  string
		actionFlag    string
		vaultAddrFlag string
	)

	flag.StringVar(&pathFlag, "path", defaultPath, "Path to Terraform code")
	flag.StringVar(&pathFlag, "p", defaultPath, "Path to Terraform code (shorthand)")
	flag.StringVar(&stackFlag, "stack", "", "Stack name (if using app/stacks structure)")
	flag.StringVar(&stackFlag, "s", "", "Stack name (shorthand)")
	flag.StringVar(&envFlag, "env", defaultEnv, "Environment name")
	flag.StringVar(&envFlag, "e", defaultEnv, "Environment name (shorthand)")
	flag.StringVar(&varsFileFlag, "vars-file", "", "Path to tfvars file")
	flag.StringVar(&varsFileFlag, "v", "", "Path to tfvars file (shorthand)")
	flag.StringVar(&actionFlag, "action", defaultAction, "Terraform action (plan, apply, destroy)")
	flag.StringVar(&vaultAddrFlag, "vault-addr", defaultVaultAddr, "Vault server address")

	flag.Parse()

	if pathFlag == "" && stackFlag == "" {
		fmt.Println("Error: either --path or --stack flag is required")
		flag.Usage()
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(envFlag)
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	var terraformPath string
	var varsFilePaths []string

	fmt.Printf("[DEBUG] pathFlag: %s, stackFlag: %s, TF_PATH: %s\n", pathFlag, stackFlag, os.Getenv("TF_PATH"))

	if stackFlag != "" {
		// Stack takes priority over path flag
		// Resolve stack path relative to TF_PATH
		basePath := os.Getenv("TF_PATH")
		if basePath == "" {
			basePath = "."
		}

		// Use the stack path directly - this points to app/stacks/{stack}
		stackPath := cfg.ResolveStackPath(stackFlag)
		terraformPath = filepath.Join(basePath, stackPath)
		fmt.Printf("Using stack path: %s\n", terraformPath)
		fmt.Printf("[DEBUG] basePath: %s, stackPath: %s, stackFlag: %s\n", basePath, stackPath, stackFlag)
	} else if pathFlag != "" {
		terraformPath = pathFlag
		fmt.Printf("[DEBUG] Using pathFlag: %s\n", terraformPath)
	} else {
		terraformPath = os.Getenv("TF_PATH")
		if terraformPath == "" {
			terraformPath = "."
		}
		fmt.Printf("[DEBUG] Using TF_PATH fallback: %s\n", terraformPath)
	}

	if varsFileFlag != "" {
		varsFilePaths = []string{varsFileFlag}
	} else {
		// For tfvars resolution, always use the base TF_PATH, not the stack-specific path
		baseTerraformPath := os.Getenv("TF_PATH")
		if baseTerraformPath == "" {
			baseTerraformPath = "."
		}
		varsFilePaths = cfg.ResolveVarsPath(envFlag, stackFlag, baseTerraformPath)
	}

	if _, err := os.Stat(terraformPath); os.IsNotExist(err) {
		fmt.Printf("Error: Terraform path does not exist: %s\n", terraformPath)
		os.Exit(1)
	}

	for _, varsFilePath := range varsFilePaths {
		if _, err := os.Stat(varsFilePath); os.IsNotExist(err) {
			fmt.Printf("Error: Vars file does not exist: %s\n", varsFilePath)
			os.Exit(1)
		}
	}

	vaultAddr := vaultAddrFlag
	if vaultAddr == "" {
		vaultAddr = cfg.Vault.Address
	}

	vaultClient, err := vault.NewClient(vaultAddr)
	if err != nil {
		fmt.Printf("Error initializing Vault client: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Authenticating with Vault...")
	err = vaultClient.Authenticate(ctx, cfg)
	if err != nil {
		fmt.Printf("Error authenticating with Vault: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Retrieving provider configuration...")
	providerPath := cfg.ResolveProviderPath(envFlag)
	providerConfig, err := vaultClient.GetProviderConfig(ctx, providerPath, envFlag)
	if err != nil {
		fmt.Printf("Error retrieving provider configuration: %v\n", err)
		os.Exit(1)
	}

	// Set AWS_PROFILE from provider config for S3 backend
	if awsConfig, ok := providerConfig["aws"].(map[string]interface{}); ok {
		if profile, ok := awsConfig["profile"].(string); ok && profile != "" {
			os.Setenv("AWS_PROFILE", profile)
			fmt.Printf("Set AWS_PROFILE to: %s\n", profile)
		}
	}

	fmt.Println("Setting up Terraform workspace...")
	executor, err := terraform.NewExecutor(ctx)
	if err != nil {
		fmt.Printf("Error creating Terraform executor: %v\n", err)
		os.Exit(1)
	}
	defer executor.Clean()

	// Always use S3 backend (equivalent to backend.rb logic)
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
	s3Config = terraform.ResolveS3BackendConfig(ctx, s3Config, envFlag, stackFlag)
	backendConfig := &s3Config

	fmt.Printf("Using S3 backend: %s/%s in %s\n", s3Config.Bucket, s3Config.Key, s3Config.Region)

	err = executor.Setup(ctx, terraformPath, providerConfig, backendConfig)
	if err != nil {
		fmt.Printf("Error setting up Terraform workspace: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Initializing Terraform...")
	err = executor.Init(ctx)
	if err != nil {
		fmt.Printf("Error initializing Terraform: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Executing Terraform %s...\n", actionFlag)
	switch actionFlag {
	case "plan":
		fmt.Println("Generating Terraform plan...")
		plan, err := executor.Plan(ctx, varsFilePaths)
		if err != nil {
			fmt.Printf("Error running Terraform plan: %v\n", err)
			os.Exit(1)
		}

		if plan.ResourceChanges != nil {
			var toAdd, toChange, toDestroy int
			var addResources, changeResources, destroyResources []string

			for _, rc := range plan.ResourceChanges {
				if rc.Change != nil {
					action := rc.Change.Actions
					resource := fmt.Sprintf("%s (%s)", rc.Address, rc.Type)

					if action.Create() {
						toAdd++
						addResources = append(addResources, resource)
					} else if action.Update() {
						toChange++
						changeResources = append(changeResources, resource)
					} else if action.Delete() {
						toDestroy++
						destroyResources = append(destroyResources, resource)
					}
				}
			}

			fmt.Printf("\nPlan: %d to add, %d to change, %d to destroy.\n", toAdd, toChange, toDestroy)

			if toAdd > 0 {
				fmt.Println("\nResources to add:")
				for _, r := range addResources {
					fmt.Printf("  + %s\n", r)
				}
			}

			if toChange > 0 {
				fmt.Println("\nResources to change:")
				for _, r := range changeResources {
					fmt.Printf("  ~ %s\n", r)
				}
			}

			if toDestroy > 0 {
				fmt.Println("\nResources to destroy:")
				for _, r := range destroyResources {
					fmt.Printf("  - %s\n", r)
				}
			}
		}

	case "apply":
		err = executor.Apply(ctx, varsFilePaths)
		if err != nil {
			fmt.Printf("Error running Terraform apply: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Apply complete!")

		outputs, err := executor.Output(ctx)
		if err != nil {
			fmt.Printf("Error getting outputs: %v\n", err)
		} else if len(outputs) > 0 {
			fmt.Println("\nOutputs:")
			for k, v := range outputs {
				fmt.Printf("%s = %v\n", k, v.Value)
			}
		}

	case "destroy":
		err = executor.Destroy(ctx, varsFilePaths)
		if err != nil {
			fmt.Printf("Error running Terraform destroy: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Destroy complete!")

	default:
		fmt.Printf("Unsupported action: %s\n", actionFlag)
		os.Exit(1)
	}

	fmt.Println("Operation completed successfully.")
}
