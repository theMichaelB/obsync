# Lambda Deployment Guide

This comprehensive guide covers deploying Obsync as an AWS Lambda function for serverless, automated vault synchronization.

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [S3 Infrastructure Setup](#s3-infrastructure-setup)
4. [IAM Configuration](#iam-configuration)
5. [Lambda Function Deployment](#lambda-function-deployment)
6. [Environment Configuration](#environment-configuration)
7. [Deployment Options](#deployment-options)
8. [Monitoring & Logging](#monitoring--logging)
9. [Troubleshooting](#troubleshooting)
10. [Cost Optimization](#cost-optimization)

## Overview

Obsync Lambda deployment enables:
- **Automated vault synchronization** on schedules or triggers
- **S3-based storage** with vault-specific organization
- **Serverless scaling** with memory-aware processing
- **State persistence** with S3 versioning for conflict resolution
- **Multi-vault support** with individual or bulk processing

### Architecture Components

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   CloudWatch    │    │   Lambda Function│    │   S3 Bucket     │
│   Events        │───►│   (obsync)       │───►│   Vault Storage │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                        
                                ▼                        
                       ┌──────────────────┐              
                       │   Obsidian API   │              
                       │   (WebSocket)    │              
                       └──────────────────┘              
```

## Prerequisites

### Required Tools
- **AWS CLI** v2+ configured with appropriate credentials
- **Go 1.24+** for building Lambda binary
- **Make** for build automation
- **Python 3.9+** (for Terraform/SAM templates)

### AWS Account Setup
- AWS account with Lambda, S3, and IAM permissions
- Configured AWS credentials (`aws configure`)
- Selected AWS region (recommend `us-east-1` for Obsidian API proximity)

### Obsidian Account Requirements
- Obsidian account with TOTP/2FA enabled
- Valid vault credentials and passwords
- API access tokens

## S3 Infrastructure Setup

### 1. Create S3 Bucket

```bash
# Create bucket (replace with unique name)
BUCKET_NAME="obsync-$(date +%s)"
aws s3 mb s3://$BUCKET_NAME --region us-east-1

# Enable versioning for state management
aws s3api put-bucket-versioning \
  --bucket $BUCKET_NAME \
  --versioning-configuration Status=Enabled

# Optional: Enable encryption
aws s3api put-bucket-encryption \
  --bucket $BUCKET_NAME \
  --server-side-encryption-configuration '{
    "Rules": [{
      "ApplyServerSideEncryptionByDefault": {
        "SSEAlgorithm": "AES256"
      }
    }]
  }'
```

### 2. Configure Bucket Structure

Obsync organizes S3 storage as:
```
s3://your-bucket/
├── vaults/
│   ├── vault-name-1/
│   │   ├── file1.md
│   │   └── subfolder/file2.md
│   └── vault-name-2/
│       └── notes.md
└── state/
    ├── vault-id-1.json
    └── vault-id-2.json
```

### 3. Lifecycle Configuration (Optional)

```bash
# Create lifecycle policy for state versioning
cat > lifecycle.json << EOF
{
  "Rules": [{
    "ID": "StateVersionCleanup",
    "Status": "Enabled",
    "Filter": {"Prefix": "state/"},
    "NoncurrentVersionExpiration": {"NoncurrentDays": 30},
    "AbortIncompleteMultipartUpload": {"DaysAfterInitiation": 7}
  }]
}
EOF

aws s3api put-bucket-lifecycle-configuration \
  --bucket $BUCKET_NAME \
  --lifecycle-configuration file://lifecycle.json
```

## IAM Configuration

### 1. Create IAM Policy

```bash
cat > obsync-lambda-policy.json << EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject",
        "s3:ListBucket",
        "s3:GetObjectVersion",
        "s3:PutObjectAcl"
      ],
      "Resource": [
        "arn:aws:s3:::$BUCKET_NAME",
        "arn:aws:s3:::$BUCKET_NAME/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:*:*:*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "xray:PutTraceSegments",
        "xray:PutTelemetryRecords"
      ],
      "Resource": "*"
    }
  ]
}
EOF

# Create policy
aws iam create-policy \
  --policy-name obsync-lambda-policy \
  --policy-document file://obsync-lambda-policy.json
```

### 2. Create IAM Role

```bash
cat > trust-policy.json << EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "lambda.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}
EOF

# Create role
aws iam create-role \
  --role-name obsync-lambda-role \
  --assume-role-policy-document file://trust-policy.json

# Attach policies
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
aws iam attach-role-policy \
  --role-name obsync-lambda-role \
  --policy-arn arn:aws:iam::$ACCOUNT_ID:policy/obsync-lambda-policy

aws iam attach-role-policy \
  --role-name obsync-lambda-role \
  --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole
```

## Lambda Function Deployment

### 1. Build Lambda Package

```bash
# Build optimized Lambda binary
make build-lambda

# Verify package contents
unzip -l build/obsync-lambda.zip
```

The build process:
- Compiles for Linux ARM64 architecture
- Strips debug symbols for size optimization
- Packages as `bootstrap` executable
- Creates deployment-ready ZIP file

### 2. Deploy Function

First, create a secret in AWS Secrets Manager with your combined credentials:

```bash
# Create secret from credentials file
aws secretsmanager create-secret \
  --name obsync-credentials \
  --secret-string file://credentials.json

# Get the secret ARN
SECRET_ARN=$(aws secretsmanager describe-secret --secret-id obsync-credentials --query ARN --output text)
```

Then deploy the Lambda function:

```bash
# Deploy Lambda function
aws lambda create-function \
  --function-name obsync-sync \
  --runtime provided.al2 \
  --architecture arm64 \
  --role arn:aws:iam::$ACCOUNT_ID:role/obsync-lambda-role \
  --handler bootstrap \
  --zip-file fileb://build/obsync-lambda.zip \
  --timeout 900 \
  --memory-size 1024 \
  --environment Variables='{
    "OBSYNC_SECRET_NAME": "'$SECRET_ARN'",
    "S3_BUCKET": "'$BUCKET_NAME'",
    "S3_PREFIX": "vaults/",
    "S3_STATE_PREFIX": "state/",
    "AWS_REGION": "us-east-1",
    "DOWNLOAD_ON_STARTUP": "true",
    "CACHE_TIMEOUT": "300",
    "LOG_LEVEL": "info"
  }' \
  --tracing-config Mode=Active \
  --dead-letter-config TargetArn=arn:aws:sqs:us-east-1:$ACCOUNT_ID:obsync-dlq
```

### 3. Test Deployment

```bash
# Test function
aws lambda invoke \
  --function-name obsync-sync \
  --payload '{"vault_id": "test", "action": "sync"}' \
  response.json

# Check logs
aws logs describe-log-groups --log-group-name-prefix "/aws/lambda/obsync-sync"
```

## Environment Configuration

### Core Environment Variables

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `OBSYNC_SECRET_NAME` | Yes | Secrets Manager name/ARN containing combined credentials | `obsync-credentials` or full ARN |
| `S3_BUCKET` | Yes | S3 bucket name | `obsync-bucket` |
| `S3_PREFIX` | No | Vault storage prefix | `vaults/` |
| `S3_STATE_PREFIX` | No | State storage prefix | `state/` |
| `AWS_REGION` | No | AWS region | `us-east-1` |

### Lambda-Specific Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DOWNLOAD_ON_STARTUP` | `true` | Pre-download state files |
| `CACHE_TIMEOUT` | `300` | Local cache timeout (seconds) |
| `MEMORY_THRESHOLD` | `0.8` | Memory usage threshold |
| `MAX_BATCH_SIZE` | `100` | Maximum files per batch |
| `STARTUP_TIMEOUT` | `120` | Startup timeout (seconds) |

### Secure Configuration Management

#### AWS Secrets Manager (Recommended)

```bash
# Create combined secret for account + vault passwords
aws secretsmanager create-secret \
  --name "obsync/combined" \
  --secret-string '{
    "auth": {
      "email": "you@example.com",
      "password": "your-password",
      "totp_secret": "YOUR_BASE32_SECRET"
    },
    "vaults": {
      "My Vault": {"password": "MyVaultPassword"},
      "Work Notes": {"password": "WorkVaultPassword"}
    }
  }'

# Configure Lambda to read the secret (env only)
aws lambda update-function-configuration \
  --function-name obsync-sync \
  --environment "Variables={OBSYNC_SECRET_NAME=arn:aws:secretsmanager:...:secret:obsync/combined-abc}"
```

## Deployment Options

### Option 1: AWS SAM Template

Create `template.yaml`:

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31

Parameters:
  BucketName:
    Type: String
    Default: !Sub 'obsync-${AWS::AccountId}'
  
  ObsyncSecretArn:
    Type: String
    Description: ARN of Secrets Manager secret containing combined credentials

Resources:
  ObsyncBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Ref BucketName
      VersioningConfiguration:
        Status: Enabled
      PublicAccessBlockConfiguration:
        BlockPublicAcls: true
        BlockPublicPolicy: true
        IgnorePublicAcls: true
        RestrictPublicBuckets: true

  ObsyncFunction:
    Type: AWS::Serverless::Function
    Properties:
      FunctionName: obsync-sync
      CodeUri: build/obsync-lambda.zip
      Handler: bootstrap
      Runtime: provided.al2
      Architectures: [arm64]
      Timeout: 900
      MemorySize: 1024
      Environment:
        Variables:
          OBSYNC_SECRET_NAME: !Ref ObsyncSecretArn
          S3_BUCKET: !Ref ObsyncBucket
          S3_PREFIX: vaults/
          S3_STATE_PREFIX: state/
          AWS_REGION: !Ref AWS::Region
          DOWNLOAD_ON_STARTUP: true
      Events:
        ScheduleEvent:
          Type: Schedule
          Properties:
            Schedule: rate(1 hour)
            Input: '{"action": "sync_all"}'

  ObsyncLogGroup:
    Type: AWS::Logs::LogGroup
    Properties:
      LogGroupName: !Sub '/aws/lambda/${ObsyncFunction}'
      RetentionInDays: 14

Outputs:
  FunctionArn:
    Description: Lambda Function ARN
    Value: !GetAtt ObsyncFunction.Arn
  
  BucketName:
    Description: S3 Bucket Name
    Value: !Ref ObsyncBucket
```

Deploy with SAM:

```bash
# Build and deploy
sam build
sam deploy --guided
```

### Option 2: Terraform Configuration

Create `main.tf`:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "obsidian_email" {
  description = "Obsidian account email"
  type        = string
}

variable "obsidian_password" {
  description = "Obsidian account password"
  type        = string
  sensitive   = true
}

variable "obsidian_totp_secret" {
  description = "TOTP secret"
  type        = string
  sensitive   = true
}

resource "aws_s3_bucket" "obsync" {
  bucket        = "obsync-${random_id.bucket_suffix.hex}"
  force_destroy = true
}

resource "random_id" "bucket_suffix" {
  byte_length = 8
}

resource "aws_s3_bucket_versioning" "obsync" {
  bucket = aws_s3_bucket.obsync.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_iam_role" "lambda_role" {
  name = "obsync-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy" "lambda_policy" {
  name = "obsync-lambda-policy"
  role = aws_iam_role.lambda_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket",
          "s3:GetObjectVersion"
        ]
        Resource = [
          aws_s3_bucket.obsync.arn,
          "${aws_s3_bucket.obsync.arn}/*"
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      }
    ]
  })
}

resource "aws_lambda_function" "obsync" {
  filename         = "build/obsync-lambda.zip"
  function_name    = "obsync-sync"
  role            = aws_iam_role.lambda_role.arn
  handler         = "bootstrap"
  source_code_hash = filebase64sha256("build/obsync-lambda.zip")
  runtime         = "provided.al2"
  architectures   = ["arm64"]
  timeout         = 900
  memory_size     = 1024

  environment {
    variables = {
      OBSYNC_SECRET_NAME  = aws_secretsmanager_secret.obsync_credentials.arn
      S3_BUCKET          = aws_s3_bucket.obsync.id
      S3_PREFIX          = "vaults/"
      S3_STATE_PREFIX    = "state/"
      AWS_REGION         = var.aws_region
      DOWNLOAD_ON_STARTUP = "true"
    }
  }

  tracing_config {
    mode = "Active"
  }
}

resource "aws_cloudwatch_event_rule" "schedule" {
  name                = "obsync-schedule"
  description         = "Trigger obsync every hour"
  schedule_expression = "rate(1 hour)"
}

resource "aws_cloudwatch_event_target" "lambda" {
  rule      = aws_cloudwatch_event_rule.schedule.name
  target_id = "obsync-target"
  arn       = aws_lambda_function.obsync.arn

  input = jsonencode({
    action = "sync_all"
  })
}

resource "aws_lambda_permission" "allow_cloudwatch" {
  statement_id  = "AllowExecutionFromCloudWatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.obsync.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.schedule.arn
}

output "function_name" {
  value = aws_lambda_function.obsync.function_name
}

output "s3_bucket" {
  value = aws_s3_bucket.obsync.id
}
```

Deploy with Terraform:

```bash
# Initialize and deploy
terraform init
terraform plan
terraform apply
```

### Option 3: Manual Deployment Script

Create `deploy.sh`:

```bash
#!/bin/bash
set -e

# Configuration
FUNCTION_NAME="obsync-sync"
REGION="us-east-1"
MEMORY_SIZE="1024"
TIMEOUT="900"

# Build Lambda package
echo "Building Lambda package..."
make build-lambda

# Check if function exists
if aws lambda get-function --function-name $FUNCTION_NAME >/dev/null 2>&1; then
    echo "Updating existing function..."
    aws lambda update-function-code \
        --function-name $FUNCTION_NAME \
        --zip-file fileb://build/obsync-lambda.zip
    
    aws lambda update-function-configuration \
        --function-name $FUNCTION_NAME \
        --memory-size $MEMORY_SIZE \
        --timeout $TIMEOUT
else
    echo "Creating new function..."
    aws lambda create-function \
        --function-name $FUNCTION_NAME \
        --runtime provided.al2 \
        --architecture arm64 \
        --role arn:aws:iam::$(aws sts get-caller-identity --query Account --output text):role/obsync-lambda-role \
        --handler bootstrap \
        --zip-file fileb://build/obsync-lambda.zip \
        --timeout $TIMEOUT \
        --memory-size $MEMORY_SIZE
fi

echo "Deployment complete!"
```

## Monitoring & Logging

### CloudWatch Dashboards

Create monitoring dashboard:

```bash
cat > dashboard.json << EOF
{
  "widgets": [
    {
      "type": "metric",
      "properties": {
        "metrics": [
          ["AWS/Lambda", "Duration", "FunctionName", "obsync-sync"],
          [".", "Errors", ".", "."],
          [".", "Invocations", ".", "."],
          [".", "Throttles", ".", "."]
        ],
        "period": 300,
        "stat": "Average",
        "region": "us-east-1",
        "title": "Lambda Metrics"
      }
    }
  ]
}
EOF

aws cloudwatch put-dashboard \
  --dashboard-name "Obsync-Lambda" \
  --dashboard-body file://dashboard.json
```

### Alarms and Notifications

```bash
# Create SNS topic for alerts
aws sns create-topic --name obsync-alerts

# Subscribe to notifications
aws sns subscribe \
  --topic-arn arn:aws:sns:us-east-1:$ACCOUNT_ID:obsync-alerts \
  --protocol email \
  --notification-endpoint your@email.com

# Create error alarm
aws cloudwatch put-metric-alarm \
  --alarm-name "obsync-lambda-errors" \
  --alarm-description "Lambda function errors" \
  --metric-name Errors \
  --namespace AWS/Lambda \
  --statistic Sum \
  --period 300 \
  --threshold 1 \
  --comparison-operator GreaterThanOrEqualToThreshold \
  --dimensions Name=FunctionName,Value=obsync-sync \
  --alarm-actions arn:aws:sns:us-east-1:$ACCOUNT_ID:obsync-alerts
```

### Log Analysis

Query logs with CloudWatch Insights:

```sql
-- Find sync errors
fields @timestamp, @message
| filter @message like /ERROR/
| sort @timestamp desc
| limit 20

-- Memory usage analysis
fields @timestamp, @message
| filter @message like /Memory usage/
| stats max(@message) by bin(5m)

-- Sync performance metrics
fields @timestamp, @message
| filter @message like /Sync completed/
| parse @message /duration=(?<duration>\S+)/
| stats avg(duration) by bin(1h)
```

## Troubleshooting

### Common Issues

#### 1. Lambda Timeout

**Symptoms**: Function times out after 15 minutes
**Solutions**:
- Increase timeout to 900 seconds (15 minutes max)
- Enable batch processing for large vaults
- Use memory-aware throttling

```bash
aws lambda update-function-configuration \
  --function-name obsync-sync \
  --timeout 900
```

#### 2. Memory Issues

**Symptoms**: Out of memory errors, slow performance
**Solutions**:
- Increase memory allocation
- Enable memory monitoring
- Reduce batch sizes

```bash
aws lambda update-function-configuration \
  --function-name obsync-sync \
  --memory-size 2048
```

#### 3. S3 Permission Errors

**Symptoms**: Access denied errors
**Solutions**:
- Verify IAM policy includes all required S3 actions
- Check bucket policy for cross-account access
- Ensure Lambda execution role is correct

#### 4. State Conflicts

**Symptoms**: Sync state corruption, version conflicts
**Solutions**:
- Enable S3 versioning
- Check ETag handling in state store
- Clear local cache and restart

```bash
# Reset function state
aws lambda update-function-configuration \
  --function-name obsync-sync \
  --environment Variables='{"CLEAR_CACHE":"true",...}'
```

### Debug Mode

Enable detailed logging:

```bash
aws lambda update-function-configuration \
  --function-name obsync-sync \
  --environment Variables='{"LOG_LEVEL":"debug",...}'
```

### Performance Tuning

#### Memory Optimization

Test different memory configurations:

```bash
# Test memory sizes
for memory in 512 1024 1536 2048; do
  aws lambda update-function-configuration \
    --function-name obsync-sync \
    --memory-size $memory
  
  # Run test and measure performance
  aws lambda invoke \
    --function-name obsync-sync \
    --payload '{"action": "sync", "vault_id": "test"}' \
    output.json
done
```

#### Concurrent Processing

Configure reserved concurrency:

```bash
aws lambda put-provisioned-concurrency-config \
  --function-name obsync-sync \
  --qualifier '$LATEST' \
  --provisioned-concurrency-value 2
```

## Cost Optimization

### Cost Factors

1. **Lambda Execution Time**: Charged per 100ms
2. **Memory Allocation**: Affects per-millisecond cost
3. **S3 Storage**: Standard storage costs
4. **S3 Requests**: GET, PUT, DELETE operations
5. **CloudWatch Logs**: Log storage and ingestion

### Optimization Strategies

#### 1. Right-size Memory

Use AWS Lambda Power Tuning tool:

```bash
# Deploy power tuning tool
git clone https://github.com/alexcasalboni/aws-lambda-power-tuning.git
cd aws-lambda-power-tuning
sam deploy --guided
```

#### 2. Optimize Sync Frequency

```bash
# Reduce sync frequency for inactive vaults
aws events put-rule \
  --name obsync-reduced-schedule \
  --schedule-expression "rate(6 hours)"
```

#### 3. S3 Storage Class Optimization

```bash
# Transition old vault files to IA
aws s3api put-bucket-lifecycle-configuration \
  --bucket $BUCKET_NAME \
  --lifecycle-configuration '{
    "Rules": [{
      "ID": "VaultStorageOptimization",
      "Status": "Enabled",
      "Filter": {"Prefix": "vaults/"},
      "Transitions": [{
        "Days": 30,
        "StorageClass": "STANDARD_IA"
      }]
    }]
  }'
```

### Cost Monitoring

Set up billing alerts:

```bash
aws budgets create-budget \
  --account-id $ACCOUNT_ID \
  --budget '{
    "BudgetName": "obsync-lambda-budget",
    "BudgetLimit": {"Amount": "10.00", "Unit": "USD"},
    "TimeUnit": "MONTHLY",
    "BudgetType": "COST"
  }' \
  --notifications-with-subscribers '[{
    "Notification": {
      "NotificationType": "ACTUAL",
      "ComparisonOperator": "GREATER_THAN",
      "Threshold": 80
    },
    "Subscribers": [{
      "SubscriptionType": "EMAIL",
      "Address": "your@email.com"
    }]
  }]'
```

## Best Practices

### Security
1. **Use IAM roles**, never embed credentials
2. **Enable encryption** for S3 bucket and Lambda environment
3. **Rotate credentials** regularly
4. **Use least privilege** IAM policies
5. **Enable CloudTrail** for audit logging

### Reliability
1. **Configure dead letter queues** for failed invocations
2. **Set up CloudWatch alarms** for monitoring
3. **Use S3 versioning** for state management
4. **Implement retry logic** with exponential backoff
5. **Test disaster recovery** procedures

### Performance
1. **Profile memory usage** and right-size functions
2. **Use provisioned concurrency** for consistent performance
3. **Enable X-Ray tracing** for performance analysis
4. **Optimize batch sizes** for large vaults
5. **Cache state locally** with appropriate timeouts

### Cost Management
1. **Monitor execution duration** and optimize code
2. **Use appropriate S3 storage classes** for different access patterns
3. **Set up billing alerts** and budgets
4. **Review CloudWatch log retention** periods
5. **Consider reserved capacity** for predictable workloads

---

This deployment guide provides comprehensive coverage of Lambda deployment options. For implementation details, see the [Lambda Implementation Guide](LAMBDA_GUIDE.md).
