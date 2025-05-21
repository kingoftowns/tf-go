package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/kingoftowns/tf-go/internal/config"
)

type Client struct {
	client *vaultapi.Client
}

func NewClient(address string) (*Client, error) {
	config := vaultapi.DefaultConfig()
	if address != "" {
		config.Address = address
	}

	client, err := vaultapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}

	return &Client{
		client: client,
	}, nil
}

func (c *Client) Authenticate(ctx context.Context, cfg *config.Config) error {
	switch cfg.Vault.AuthMethod {
	case "token":
		// Try to get token from environment variable first
		token := os.Getenv("VAULT_TOKEN")
		if token == "" {
			// Fallback to reading from token file
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}

			tokenPath := filepath.Join(homeDir, ".vault-token")
			if _, err := os.Stat(tokenPath); err == nil {
				tokenData, err := os.ReadFile(tokenPath)
				if err != nil {
					return fmt.Errorf("failed to read token file: %w", err)
				}
				token = string(tokenData)
			}
		}

		if token == "" {
			return fmt.Errorf("no Vault token found")
		}

		c.client.SetToken(token)
		return nil

	case "approle":
		// Check for role ID and secret ID
		roleID := os.Getenv("VAULT_ROLE_ID")
		if roleID == "" && cfg.Vault.RoleName != "" {
			roleID = cfg.Vault.RoleName
		}

		secretID := os.Getenv("VAULT_SECRET_ID")
		if secretID == "" && cfg.Vault.SecretID != "" {
			secretID = cfg.Vault.SecretID
		}

		if roleID == "" || secretID == "" {
			return fmt.Errorf("missing role ID or secret ID for AppRole authentication")
		}

		data := map[string]interface{}{
			"role_id":   roleID,
			"secret_id": secretID,
		}

		resp, err := c.client.Logical().Write("auth/approle/login", data)
		if err != nil {
			return fmt.Errorf("failed to authenticate with AppRole: %w", err)
		}

		c.client.SetToken(resp.Auth.ClientToken)
		return nil

	default:
		return fmt.Errorf("unsupported authentication method: %s", cfg.Vault.AuthMethod)
	}
}

// GetProviderConfig retrieves provider configuration from Vault
func (c *Client) GetProviderConfig(ctx context.Context, path string) (map[string]interface{}, error) {
	secret, err := c.client.Logical().Read(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read from Vault: %w", err)
	}

	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("no data found at path: %s", path)
	}

	return secret.Data, nil
}
