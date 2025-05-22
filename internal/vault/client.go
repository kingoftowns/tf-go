package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/kingoftowns/tf-go/internal/config"
)

func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

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
		token := os.Getenv("VAULT_TOKEN")
		fmt.Printf("DEBUG: Token from env: %s\n", token)
		if token == "" {
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
		fmt.Printf("DEBUG: Token authentication successful\n")
		return nil

	default:
		return fmt.Errorf("unsupported authentication method: %s", cfg.Vault.AuthMethod)
	}
}

func (c *Client) GetProviderConfig(ctx context.Context, path, env string) (map[string]interface{}, error) {
	vaultAddr := os.Getenv("VAULT_ADDR")
	if vaultAddr == "" {
		vaultAddr = "http://127.0.0.1:8200"
	}

	url := vaultAddr + "/v1/" + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	token := os.Getenv("VAULT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("VAULT_TOKEN not set")
	}
	req.Header.Set("X-Vault-Token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vault returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var vaultResponse map[string]interface{}
	if err := json.Unmarshal(body, &vaultResponse); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Navigate to data.data.{{env}}
	data, ok := vaultResponse["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response structure: no 'data' key")
	}

	nestedData, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response structure: no nested 'data' key")
	}

	envData, exists := nestedData[env]
	if !exists {
		return nil, fmt.Errorf("no configuration found for environment: %s", env)
	}

	if jsonStr, ok := envData.(string); ok {
		var config map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON configuration for env %s: %w", env, err)
		}
		return config, nil
	}

	if configMap, ok := envData.(map[string]interface{}); ok {
		return configMap, nil
	}

	return nil, fmt.Errorf("unexpected data type for environment %s", env)
}
