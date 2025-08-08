# AWS Deployment Quickstart

This guide packages the existing Lambda docs into a simple, copy‑pasteable path for new users. For deep details, see `docs/LAMBDA_DEPLOYMENT.md`.

## Prerequisites
- AWS account + AWS CLI v2 configured (`aws sts get-caller-identity` works)
- Go 1.24+, Make
- Obsidian credentials (email, password, TOTP secret)

## TL;DR (10 minutes)
1) Build the Lambda package
```bash
make build-lambda
```
2) Use Terraform example (recommended)
```bash
cd terraform/obsync-lambda
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your values
terraform init && terraform apply
```
3) Invoke and check logs
```bash
aws lambda invoke --function-name obsync-sync out.json && cat out.json
aws logs tail "/aws/lambda/obsync-sync" --follow
```

## What gets created (Terraform)
- S3 bucket (versioned) for vault data and sync state
- IAM role/policy for the function (S3 + CloudWatch Logs)
- Lambda function (`obsync-sync`) using the packaged `bootstrap`
- Optional CloudWatch schedule (disabled by default)

## Manual deploy (no Terraform)
Use the helper script (creates/updates code only; role/bucket must exist):
```bash
./scripts/deploy-lambda.sh \
  --function obsync-sync \
  --role arn:aws:iam::<ACCOUNT_ID>:role/obsync-lambda-role \
  --region us-east-1 \
  --env OBSIDIAN_EMAIL=you@example.com \
  --env OBSIDIAN_PASSWORD=secret \
  --env OBSIDIAN_TOTP_SECRET=BASE32 \
  --env S3_BUCKET=your-bucket --env S3_PREFIX=vaults/ --env S3_STATE_PREFIX=state/
```

## Next steps
- Configure scheduling in Terraform (`enable_schedule = true`)
- Tune memory/timeout in variables
- Review security tips in `SECURITY.md`

Troubleshooting and advanced options: `docs/LAMBDA_DEPLOYMENT.md`.

## Credentials Model
- Account credentials: used to authenticate to Obsidian.
  - Lambda: set `OBSIDIAN_EMAIL`, `OBSIDIAN_PASSWORD`, and optionally `OBSIDIAN_TOTP_SECRET` as environment variables (or store in Secrets Manager/SSM and expose as envs).
  - CLI: run `obsync login --email ...` (prompts for password/TOTP) or set them in `config.json` under `auth`.
- Vault credentials: per‑vault password used to derive the vault encryption key.
  - CLI: pass with `--password` or let the tool prompt. For non‑interactive runs, you can also provide a file and reference it from `config.json`:
    - `auth.vault_credentials_file`: path to a JSON file mapping vault IDs to passwords. Example:
      ```json
      {"vaults": {"vault-abc123": {"password": "MyVaultPassword"}}}
      ```
      The CLI will pick this up automatically if present.
  - Lambda: retrieving vault passwords securely (e.g., from Secrets Manager) should be wired in the handler for your environment. Until that is in place, prefer running the CLI in `--s3` mode within a scheduled job (ECS/EKS/EC2/GitHub Actions) where you can pass `--password` from a secret.
