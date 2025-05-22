// cmd/deploy/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/kingoftowns/tf-go/internal/config"
	"github.com/kingoftowns/tf-go/internal/terraform"
	"github.com/kingoftowns/tf-go/internal/vault"
)

func main() {
	ctx := context.Background()

	defaultPath := os.Getenv("TF_PATH")
	defaultEnv := os.Getenv("TF_ENV")
	if defaultEnv == "" {
		defaultEnv = "dev-devops"
	}
	defaultAction := os.Getenv("TF_ACTION")
	if defaultAction == "" {
		defaultAction = "plan"
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

	var terraformPath, varsFilePath string

	if pathFlag != "" {
		terraformPath = pathFlag
	} else {
		terraformPath = cfg.ResolveStackPath(stackFlag)
	}

	if varsFileFlag != "" {
		varsFilePath = varsFileFlag
	} else if stackFlag != "" {
		varsFilePath = cfg.ResolveVarsPath(envFlag, stackFlag)
	}

	if _, err := os.Stat(terraformPath); os.IsNotExist(err) {
		fmt.Printf("Error: Terraform path does not exist: %s\n", terraformPath)
		os.Exit(1)
	}

	if varsFilePath != "" {
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

	fmt.Println("Setting up Terraform workspace...")
	executor, err := terraform.NewExecutor(ctx)
	if err != nil {
		fmt.Printf("Error creating Terraform executor: %v\n", err)
		os.Exit(1)
	}
	defer executor.Clean()

	var backendConfig *terraform.S3BackendConfig

	if envConfig, ok := cfg.Environments[envFlag]; ok && envConfig.Backend.Type == "s3" {
		s3Config := terraform.S3BackendConfig{
			Bucket:  fmt.Sprintf("%v", envConfig.Backend.Config["bucket"]),
			Key:     fmt.Sprintf("%v", envConfig.Backend.Config["key"]),
			Region:  fmt.Sprintf("%v", envConfig.Backend.Config["region"]),
			Encrypt: true,
		}

		if dynamo, ok := envConfig.Backend.Config["dynamodb_table"]; ok {
			s3Config.DynamoDBTable = fmt.Sprintf("%v", dynamo)
		}

		s3Config = terraform.ResolveS3BackendConfig(ctx, s3Config, envFlag, stackFlag)
		backendConfig = &s3Config

		fmt.Printf("Using S3 backend: %s/%s in %s\n", s3Config.Bucket, s3Config.Key, s3Config.Region)
	}

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
		plan, err := executor.Plan(ctx, varsFilePath)
		if err != nil {
			fmt.Printf("Error running Terraform plan: %v\n", err)
			os.Exit(1)
		}

		if plan.ResourceChanges != nil {
			var toAdd, toChange, toDestroy int
			for _, rc := range plan.ResourceChanges {
				if rc.Change != nil {
					action := rc.Change.Actions
					if action.Create() {
						toAdd++
					} else if action.Update() {
						toChange++
					} else if action.Delete() {
						toDestroy++
					}
				}
			}
			fmt.Printf("Plan: %d to add, %d to change, %d to destroy.\n", toAdd, toChange, toDestroy)
		}

	case "apply":
		err = executor.Apply(ctx, varsFilePath)
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
		err = executor.Destroy(ctx, varsFilePath)
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
