# Obsync Lambda (Terraform Example)

Provision a minimal, production-ready Lambda deployment for Obsync with S3 storage and optional scheduling.

## Usage
```bash
# From repo root
make build-lambda

cd terraform/obsync-lambda
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars

terraform init
terraform apply
```

Outputs include the Lambda function name and S3 bucket name.

## Variables (common)
- `aws_region`: AWS region (default `us-east-1`)
- `lambda_zip_path`: Path to built ZIP (default `../../build/obsync-lambda.zip`)
- `function_name`: Lambda name (default `obsync-sync`)
- `memory_size`: MB (default `1024`), `timeout`: seconds (default `900`)
- `enable_schedule`: Creates hourly CloudWatch schedule when `true`
- `schedule_expression`: Change schedule (default `rate(1 hour)`)

## Credentials
Provide Obsidian credentials and TOTP secret via Terraform variables. Consider using AWS SSM/Secrets Manager for production.
