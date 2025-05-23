# Using CLI Variables with tf-go

The tf-go deploy command now supports passing arbitrary key/value parameters via the `-var` flag. This allows you to override or set Terraform variables at runtime without modifying your tfvars files.

## Usage

```bash
# Single variable
tf-go deploy -p /path/to/terraform -var "image_tag=v1.2.3"

# Multiple variables
tf-go deploy -p /path/to/terraform \
  -var "image_tag=v1.2.3" \
  -var "environment=production" \
  -var "instance_count=3"

# Different types of values
tf-go deploy -p /path/to/terraform \
  -var "image_tag=v1.2.3" \              # String
  -var "enable_monitoring=true" \        # Boolean
  -var "instance_count=3" \              # Number
  -var "tags={env:prod,team:platform}"  # Map (simple JSON-like syntax)
  -var "availability_zones=[us-east-1a,us-east-1b]"  # List

# With stack and environment
tf-go deploy -s my-app -e production \
  -var "image_tag=v1.2.3" \
  -var "replicas=5"
```

## Variable Priority

Variables are applied in the following order (later ones override earlier ones):

1. Default values from `variables.tf` files
2. Values from tfvars files (in order specified)
3. CLI variables (highest priority)

## Examples

### Setting an image tag for deployment
```bash
tf-go deploy -s my-app -e production -var "image_tag=v2.0.0-rc1"
```

### Overriding multiple configuration values
```bash
tf-go deploy -p /terraform/infrastructure \
  -var "database_instance_class=db.t3.large" \
  -var "min_size=2" \
  -var "max_size=10" \
  -var "desired_capacity=4"
```

### Setting map values
```bash
# Simple map syntax
tf-go deploy -s my-app -e staging \
  -var "tags={environment:staging,application:myapp,version:2.0}"
```

### Setting list values
```bash
# Simple list syntax
tf-go deploy -s my-app -e production \
  -var "allowed_cidrs=[10.0.0.0/16,192.168.0.0/24]"
```

## Notes

- The `-var` flag can be used multiple times to set multiple variables
- Variables set via CLI have the highest priority and will override any values in tfvars files
- Boolean values should be specified as `true` or `false` (without quotes)
- Numbers are automatically detected and parsed appropriately
- For complex data structures, use the simple JSON-like syntax shown above