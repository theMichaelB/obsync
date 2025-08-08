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
