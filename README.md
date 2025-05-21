# tf-go

Terraform deployment tool for managing infrastructure with Hashicorp Vault integration.

## Features

- Retrieve provider configuration securely from Vault
- Support for AWS, Kubernetes, and Helm providers
- Environment variable resolution with VS Code launch.json
- Dynamic configuration resolution for EKS clusters
- Support for SSO profiles and IRSA for AWS authentication

## Configuration

### Vault Provider Configuration

The tool retrieves provider configurations from Vault. Here are examples for different environments:

#### Local Development with SSO Profile

```json
{
  "aws": {
    "region": "us-gov-west-1",
    "profile": "${ENV:AWS_PROFILE}",
    "default_tags": {
      "application": "k8s-cluster-info",
      "team": "DevOps",
      "owner": "${ENV:OWNER}"
    }
  },
  "kubernetes": {
    "config_path": "~/.kube/config",
    "config_context": "${ENV:TS_ENV}"
  },
  "helm": {
    "kubernetes": {
      "config_path": "~/.kube/config",
      "config_context": "${ENV:TS_ENV}"
    }
  }
}
```

#### CI/CD for AWS EKS

```json
{
  "aws": {
    "region": "us-gov-west-1",
    "default_tags": {
      "application": "k8s-cluster-info",
      "team": "DevOps",
      "owner": "GitLab CI"
    }
  },
  "kubernetes": {
    "host": "${DYNAMIC:EKS_CLUSTER_ENDPOINT}",
    "token": "${DYNAMIC:EKS_CLUSTER_TOKEN}",
    "cluster_ca_certificate": "${DYNAMIC:EKS_CLUSTER_CA}"
  },
  "helm": {
    "kubernetes": {
      "host": "${DYNAMIC:EKS_CLUSTER_ENDPOINT}",
      "token": "${DYNAMIC:EKS_CLUSTER_TOKEN}",
      "cluster_ca_certificate": "${DYNAMIC:EKS_CLUSTER_CA}"
    }
  }
}
```

### S3 Backend Configuration

The tool can automatically create and configure S3 buckets for Terraform state storage. Define your backend configuration in the environment-specific config like this:

```yaml
environments:
  dev:
    name: Development
    description: Dev environment
    vault:
      provider_path: kv/terraform/providers/dev
    backend:
      type: s3
      config:
        bucket: "terraform-state-myproject-:ENV"
        key: ":ENV/:STACK/terraform.tfstate"
        region: "us-east-1"
        dynamodb_table: "terraform-locks-myproject"
```

When running the tool, it will:
1. Check if the S3 bucket exists and create it if not
2. Enable versioning and encryption on the bucket
3. Create the DynamoDB table for state locking if it doesn't exist
4. Configure Terraform to use this backend

The `:ENV` and `:STACK` placeholders will be replaced with the actual environment and stack names.

### Environment Variables

For VS Code debugging, you can set environment variables in your launch.json file:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Launch",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/cmd/deploy/main.go",
      "env": {
        "AWS_PROFILE": "your-sso-profile",
        "TS_ENV": "your-k8s-context-name"
      },
      "args": []
    }
  ]
}
```

### Variable Substitution

The tool supports two types of variable substitution:

1. Environment Variables: `${ENV:VARIABLE_NAME}`
2. Dynamic Values: `${DYNAMIC:VALUE_TYPE}`

Supported dynamic value types:
- `EKS_CLUSTER_ENDPOINT`: Retrieves the API endpoint for an EKS cluster
- `EKS_CLUSTER_TOKEN`: Generates a token for EKS authentication
- `EKS_CLUSTER_CA`: Retrieves the CA certificate for an EKS cluster

## Usage

TODO: Add usage examples