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
	// Create a context
	ctx := context.Background()

	// Define command-line flags
	var (
		pathFlag      = flag.String("path", "", "Path to Terraform code")
		stackFlag     = flag.String("stack", "", "Stack name (if using app/stacks structure)")
		envFlag       = flag.String("env", "dev", "Environment name")
		varsFileFlag  = flag.String("vars-file", "", "Path to tfvars file")
		actionFlag    = flag.String("action", "plan", "Terraform action (plan, apply, destroy)")
		vaultAddrFlag = flag.String("vault-addr", "", "Vault server address")
		debugFlag     = flag.Bool("debug", false, "Enable debug mode")
	)

	// Custom short flags
	flag.StringVar(pathFlag, "p", "", "Path to Terraform code (shorthand)")
	flag.StringVar(stackFlag, "s", "", "Stack name (shorthand)")
	flag.StringVar(envFlag, "e", "dev", "Environment name (shorthand)")
	flag.StringVar(varsFileFlag, "v", "", "Path to tfvars file (shorthand)")

	// Parse flags
	flag.Parse()

	// Validate required inputs
	if *pathFlag == "" && *stackFlag == "" {
		fmt.Println("Error: either --path or --stack flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*envFlag)
	if err != nil {
		fmt.Printf("Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Determine paths
	var terraformPath, varsFilePath string

	if *pathFlag != "" {
		terraformPath = *pathFlag
	} else {
		// Use stack path template from config
		terraformPath = cfg.ResolveStackPath(*stackFlag)
	}

	if *varsFileFlag != "" {
		varsFilePath = *varsFileFlag
	} else if *stackFlag != "" {
		// Use vars path template from config
		varsFilePath = cfg.ResolveVarsPath(*envFlag, *stackFlag)
	}

	// Validate paths
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

	// Initialize Vault client
	vaultAddr := *vaultAddrFlag
	if vaultAddr == "" {
		vaultAddr = cfg.Vault.Address
	}

	vaultClient, err := vault.NewClient(vaultAddr)
	if err != nil {
		fmt.Printf("Error initializing Vault client: %v\n", err)
		os.Exit(1)
	}

	// Authenticate with Vault
	// This would need to be implemented based on your auth method
	fmt.Println("Authenticating with Vault...")
	err = vaultClient.Authenticate(ctx, cfg)
	if err != nil {
		fmt.Printf("Error authenticating with Vault: %v\n", err)
		os.Exit(1)
	}

	// Get provider configuration from Vault
	fmt.Println("Retrieving provider configuration...")
	providerPath := cfg.ResolveProviderPath(*envFlag)
	providerConfig, err := vaultClient.GetProviderConfig(ctx, providerPath)
	if err != nil {
		fmt.Printf("Error retrieving provider configuration: %v\n", err)
		os.Exit(1)
	}

	// Create Terraform executor
	fmt.Println("Setting up Terraform workspace...")
	executor, err := terraform.NewExecutor(ctx)
	if err != nil {
		fmt.Printf("Error creating Terraform executor: %v\n", err)
		os.Exit(1)
	}
	defer executor.Clean()

	// Setup Terraform workspace
	err = executor.Setup(ctx, terraformPath, providerConfig)
	if err != nil {
		fmt.Printf("Error setting up Terraform workspace: %v\n", err)
		os.Exit(1)
	}

	// Initialize Terraform
	fmt.Println("Initializing Terraform...")
	err = executor.Init(ctx)
	if err != nil {
		fmt.Printf("Error initializing Terraform: %v\n", err)
		os.Exit(1)
	}

	// Execute requested action
	fmt.Printf("Executing Terraform %s...\n", *actionFlag)
	switch *actionFlag {
	case "plan":
		plan, err := executor.Plan(ctx, varsFilePath)
		if err != nil {
			fmt.Printf("Error running Terraform plan: %v\n", err)
			os.Exit(1)
		}

		// Print plan summary
		if plan.PlannedValues != nil {
			fmt.Printf("Plan: %d to add, %d to change, %d to destroy.\n",
				len(plan.ResourceChanges.Add()),
				len(plan.ResourceChanges.Update()),
				len(plan.ResourceChanges.Destroy()),
			)
		}

	case "apply":
		err = executor.Apply(ctx, varsFilePath)
		if err != nil {
			fmt.Printf("Error running Terraform apply: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Apply complete!")

		// Print outputs
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
		fmt.Printf("Unsupported action: %s\n", *actionFlag)
		os.Exit(1)
	}

	fmt.Println("Operation completed successfully.")
}
