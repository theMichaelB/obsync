# AWS Lambda Deployment Plan for Obsync

## Overview

This document outlines the implementation plan for deploying Obsync as a serverless application on AWS Lambda. The architecture will support synchronizing individual vaults or all vaults, with both incremental and complete sync options.

## Architecture Overview

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│   API Gateway   │────▶│    Lambda    │────▶│   Obsidian API  │
│   (REST/HTTP)   │     │   Function   │     │                 │
└─────────────────┘     └──────────────┘     └─────────────────┘
         │                      │
         │                      ▼
         │              ┌──────────────┐
         │              │      S3      │
         │              │  (Storage)   │
         │              └──────────────┘
         │                      │
         ▼                      ▼
┌─────────────────┐     ┌──────────────┐
│   CloudWatch    │     │   DynamoDB   │
│   (Logging)     │     │   (State)    │
└─────────────────┘     └──────────────┘
```

## Phase 1: Lambda Function Adaptation

### 1.1 Handler Implementation

Create a Lambda handler that wraps the existing Obsync functionality:

```go
// cmd/lambda/main.go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    
    "github.com/aws/aws-lambda-go/lambda"
    "github.com/TheMichaelB/obsync/internal/lambdahandler"
)

type SyncRequest struct {
    // Sync configuration
    VaultID    string `json:"vault_id,omitempty"`    // Empty for all vaults
    SyncType   string `json:"sync_type"`             // "complete" or "incremental"
    
    // Authentication (from environment or request)
    Email      string `json:"email,omitempty"`
    Password   string `json:"password,omitempty"`
    TOTPSecret string `json:"totp_secret,omitempty"`
    
    // S3 configuration
    DestBucket string `json:"dest_bucket"`
    DestPrefix string `json:"dest_prefix,omitempty"`
}

type SyncResponse struct {
    Success      bool                   `json:"success"`
    VaultsSynced []string              `json:"vaults_synced"`
    FilesCount   int                   `json:"files_count"`
    Duration     string                `json:"duration"`
    Errors       []string              `json:"errors,omitempty"`
    Details      map[string]interface{} `json:"details,omitempty"`
}

func handler(ctx context.Context, request SyncRequest) (SyncResponse, error) {
    h := lambdahandler.New()
    return h.HandleSync(ctx, request)
}

func main() {
    lambda.Start(handler)
}
```

### 1.2 Configuration Management

Adapt configuration for Lambda environment:

```go
// internal/lambdaconfig/config.go
package lambdaconfig

import (
    "os"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/config"
)

// LoadLambdaConfig creates config from environment variables
func LoadLambdaConfig() (*config.Config, error) {
    cfg := &config.Config{
        API: config.APIConfig{
            BaseURL:    getEnvOrDefault("OBSIDIAN_API_URL", "https://api.obsidian.md"),
            Timeout:    30 * time.Second,
            MaxRetries: 3,
            UserAgent:  "obsync-lambda/1.0",
        },
        Auth: config.AuthConfig{
            Email:      os.Getenv("OBSIDIAN_EMAIL"),
            Password:   os.Getenv("OBSIDIAN_PASSWORD"),
            TOTPSecret: os.Getenv("OBSIDIAN_TOTP_SECRET"),
        },
        Storage: config.StorageConfig{
            DataDir:     "/tmp/obsync",
            StateDir:    "/tmp/obsync/state",
            TempDir:     "/tmp/obsync/temp",
            MaxFileSize: 500 * 1024 * 1024, // 500MB Lambda tmp limit
        },
        Sync: config.SyncConfig{
            ChunkSize:         1024 * 1024,
            MaxConcurrent:     10,
            RetryAttempts:     3,
            RetryDelay:        time.Second,
            ProgressInterval:  time.Second,
            ValidateChecksums: true,
        },
    }
    
    return cfg, cfg.Validate()
}
```

## Phase 2: State Management with DynamoDB

### 2.1 DynamoDB State Store

Implement a DynamoDB-backed state store for Lambda:

```go
// internal/state/dynamodb_store.go
package state

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "github.com/TheMichaelB/obsync/internal/models"
)

type DynamoDBStore struct {
    client    *dynamodb.Client
    tableName string
    ttlDays   int
}

func NewDynamoDBStore(client *dynamodb.Client, tableName string) *DynamoDBStore {
    return &DynamoDBStore{
        client:    client,
        tableName: tableName,
        ttlDays:   30, // Auto-expire old states
    }
}

func (s *DynamoDBStore) Load(vaultID string) (*models.SyncState, error) {
    ctx := context.Background()
    
    result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
        TableName: aws.String(s.tableName),
        Key: map[string]types.AttributeValue{
            "vault_id": &types.AttributeValueMemberS{Value: vaultID},
        },
    })
    
    if err != nil {
        return nil, fmt.Errorf("dynamodb: get item: %w", err)
    }
    
    if result.Item == nil {
        return models.NewSyncState(), nil
    }
    
    var state models.SyncState
    stateJSON := result.Item["state"].(*types.AttributeValueMemberS).Value
    if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
        return nil, fmt.Errorf("dynamodb: unmarshal state: %w", err)
    }
    
    return &state, nil
}

func (s *DynamoDBStore) Save(vaultID string, state *models.SyncState) error {
    ctx := context.Background()
    
    stateJSON, err := json.Marshal(state)
    if err != nil {
        return fmt.Errorf("dynamodb: marshal state: %w", err)
    }
    
    ttl := time.Now().Add(time.Duration(s.ttlDays) * 24 * time.Hour).Unix()
    
    _, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String(s.tableName),
        Item: map[string]types.AttributeValue{
            "vault_id":     &types.AttributeValueMemberS{Value: vaultID},
            "state":        &types.AttributeValueMemberS{Value: string(stateJSON)},
            "last_updated": &types.AttributeValueMemberN{Value: fmt.Sprint(time.Now().Unix())},
            "ttl":          &types.AttributeValueMemberN{Value: fmt.Sprint(ttl)},
        },
    })
    
    return err
}
```

## Phase 3: S3 Storage Integration

### 3.1 S3 Blob Store

Implement S3-backed storage for vault files:

```go
// internal/storage/s3_store.go
package storage

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "os"
    "path"
    "strings"
    "time"
    
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Store struct {
    client     *s3.Client
    bucket     string
    prefix     string
    tempDir    string
}

func NewS3Store(client *s3.Client, bucket, prefix string) *S3Store {
    return &S3Store{
        client:  client,
        bucket:  bucket,
        prefix:  prefix,
        tempDir: "/tmp/obsync/s3-cache",
    }
}

func (s *S3Store) Write(filePath string, data []byte, mode os.FileMode) error {
    ctx := context.Background()
    key := s.getS3Key(filePath)
    
    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket:      aws.String(s.bucket),
        Key:         aws.String(key),
        Body:        bytes.NewReader(data),
        ContentType: aws.String(detectContentType(filePath)),
        Metadata: map[string]string{
            "file-mode":    fmt.Sprintf("%o", mode),
            "sync-time":    time.Now().UTC().Format(time.RFC3339),
            "content-hash": calculateHash(data),
        },
    })
    
    return err
}

func (s *S3Store) Read(filePath string) ([]byte, error) {
    ctx := context.Background()
    key := s.getS3Key(filePath)
    
    result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(key),
    })
    
    if err != nil {
        return nil, err
    }
    defer result.Body.Close()
    
    return io.ReadAll(result.Body)
}

func (s *S3Store) getS3Key(filePath string) string {
    cleanPath := strings.TrimPrefix(filePath, "/")
    if s.prefix != "" {
        return path.Join(s.prefix, cleanPath)
    }
    return cleanPath
}
```

## Phase 4: Lambda Handler Implementation

### 4.1 Main Handler Logic

```go
// internal/lambdahandler/handler.go
package lambdahandler

import (
    "context"
    "fmt"
    "time"
    
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/TheMichaelB/obsync/internal/client"
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/lambdaconfig"
    "github.com/TheMichaelB/obsync/internal/state"
    "github.com/TheMichaelB/obsync/internal/storage"
)

type Handler struct {
    obsyncClient *client.Client
    s3Store      storage.BlobStore
    stateStore   state.Store
    logger       *events.Logger
}

func New() (*Handler, error) {
    // Load AWS config
    awsCfg, err := config.LoadDefaultConfig(context.Background())
    if err != nil {
        return nil, fmt.Errorf("load aws config: %w", err)
    }
    
    // Create AWS clients
    s3Client := s3.NewFromConfig(awsCfg)
    dynamoClient := dynamodb.NewFromConfig(awsCfg)
    
    // Load Obsync config
    cfg, err := lambdaconfig.LoadLambdaConfig()
    if err != nil {
        return nil, fmt.Errorf("load config: %w", err)
    }
    
    // Create logger
    logger := events.NewCloudWatchLogger()
    
    // Create state store
    tableName := getEnvOrDefault("STATE_TABLE_NAME", "obsync-state")
    stateStore := state.NewDynamoDBStore(dynamoClient, tableName)
    
    // Create S3 store
    bucket := os.Getenv("S3_BUCKET")
    if bucket == "" {
        return nil, fmt.Errorf("S3_BUCKET environment variable required")
    }
    s3Store := storage.NewS3Store(s3Client, bucket, "")
    
    // Create Obsync client with custom storage
    obsyncClient, err := client.NewWithCustomStorage(cfg, logger, s3Store, stateStore)
    if err != nil {
        return nil, fmt.Errorf("create client: %w", err)
    }
    
    return &Handler{
        obsyncClient: obsyncClient,
        s3Store:      s3Store,
        stateStore:   stateStore,
        logger:       logger,
    }, nil
}

func (h *Handler) HandleSync(ctx context.Context, req SyncRequest) (SyncResponse, error) {
    start := time.Now()
    
    // Override S3 destination if specified
    if req.DestBucket != "" {
        h.s3Store = storage.NewS3Store(s3Client, req.DestBucket, req.DestPrefix)
    }
    
    // Authenticate if credentials provided
    if req.Email != "" && req.Password != "" {
        if err := h.authenticate(ctx, req); err != nil {
            return SyncResponse{
                Success: false,
                Errors:  []string{fmt.Sprintf("authentication failed: %v", err)},
            }, nil
        }
    }
    
    // Perform sync
    var vaultsSynced []string
    var totalFiles int
    var errors []string
    
    if req.VaultID != "" {
        // Sync single vault
        count, err := h.syncVault(ctx, req.VaultID, req.SyncType == "complete")
        if err != nil {
            errors = append(errors, fmt.Sprintf("vault %s: %v", req.VaultID, err))
        } else {
            vaultsSynced = append(vaultsSynced, req.VaultID)
            totalFiles += count
        }
    } else {
        // Sync all vaults
        vaults, err := h.obsyncClient.ListVaults(ctx)
        if err != nil {
            return SyncResponse{
                Success: false,
                Errors:  []string{fmt.Sprintf("list vaults failed: %v", err)},
            }, nil
        }
        
        for _, vault := range vaults {
            count, err := h.syncVault(ctx, vault.ID, req.SyncType == "complete")
            if err != nil {
                errors = append(errors, fmt.Sprintf("vault %s: %v", vault.ID, err))
            } else {
                vaultsSynced = append(vaultsSynced, vault.ID)
                totalFiles += count
            }
        }
    }
    
    return SyncResponse{
        Success:      len(errors) == 0,
        VaultsSynced: vaultsSynced,
        FilesCount:   totalFiles,
        Duration:     time.Since(start).String(),
        Errors:       errors,
        Details: map[string]interface{}{
            "sync_type":    req.SyncType,
            "dest_bucket":  req.DestBucket,
            "dest_prefix":  req.DestPrefix,
            "lambda_memory": os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"),
        },
    }, nil
}

func (h *Handler) syncVault(ctx context.Context, vaultID string, complete bool) (int, error) {
    h.logger.WithField("vault_id", vaultID).Info("Starting vault sync")
    
    progress := make(chan client.SyncProgress, 100)
    errChan := make(chan error, 1)
    
    go func() {
        err := h.obsyncClient.SyncVault(ctx, vaultID, complete, progress)
        errChan <- err
    }()
    
    var fileCount int
    for p := range progress {
        fileCount = p.FilesProcessed
        h.logger.WithFields(map[string]interface{}{
            "files_processed": p.FilesProcessed,
            "total_files":     p.TotalFiles,
            "phase":           p.Phase,
        }).Debug("Sync progress")
    }
    
    if err := <-errChan; err != nil {
        return fileCount, err
    }
    
    return fileCount, nil
}
```

## Phase 5: API Gateway Integration

### 5.1 API Gateway Configuration

```yaml
# serverless.yml or SAM template
AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31

Globals:
  Function:
    Timeout: 900  # 15 minutes max
    MemorySize: 3008  # Maximum Lambda memory for better performance
    Environment:
      Variables:
        OBSIDIAN_API_URL: https://api.obsidian.md
        STATE_TABLE_NAME: !Ref StateTable
        S3_BUCKET: !Ref StorageBucket

Resources:
  ObsyncFunction:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: cmd/lambda/
      Handler: main
      Runtime: go1.x
      Environment:
        Variables:
          OBSIDIAN_EMAIL: !Ref ObsidianEmail
          OBSIDIAN_PASSWORD: !Ref ObsidianPassword
          OBSIDIAN_TOTP_SECRET: !Ref ObsidianTOTPSecret
      Policies:
        - S3CrudPolicy:
            BucketName: !Ref StorageBucket
        - DynamoDBCrudPolicy:
            TableName: !Ref StateTable
        - Statement:
          - Effect: Allow
            Action:
              - logs:CreateLogGroup
              - logs:CreateLogStream
              - logs:PutLogEvents
            Resource: "*"
      Events:
        SyncAPI:
          Type: Api
          Properties:
            Path: /sync
            Method: POST
            
  SyncSchedule:
    Type: AWS::Events::Rule
    Properties:
      Description: "Periodic sync of all vaults"
      ScheduleExpression: "rate(6 hours)"
      State: ENABLED
      Targets:
        - Arn: !GetAtt ObsyncFunction.Arn
          Id: "ObsyncScheduledTarget"
          Input: |
            {
              "sync_type": "incremental"
            }
            
  StateTable:
    Type: AWS::DynamoDB::Table
    Properties:
      BillingMode: PAY_PER_REQUEST
      AttributeDefinitions:
        - AttributeName: vault_id
          AttributeType: S
      KeySchema:
        - AttributeName: vault_id
          KeyType: HASH
      TimeToLiveSpecification:
        AttributeName: ttl
        Enabled: true
        
  StorageBucket:
    Type: AWS::S3::Bucket
    Properties:
      VersioningConfiguration:
        Status: Enabled
      LifecycleConfiguration:
        Rules:
          - Id: DeleteOldVersions
            NoncurrentVersionExpirationInDays: 30
            Status: Enabled
```

### 5.2 API Request Examples

```bash
# Sync single vault (complete)
curl -X POST https://api-id.execute-api.region.amazonaws.com/prod/sync \
  -H "Content-Type: application/json" \
  -d '{
    "vault_id": "vault-123",
    "sync_type": "complete",
    "dest_bucket": "my-obsidian-backup",
    "dest_prefix": "vaults/personal"
  }'

# Sync all vaults (incremental)
curl -X POST https://api-id.execute-api.region.amazonaws.com/prod/sync \
  -H "Content-Type: application/json" \
  -d '{
    "sync_type": "incremental"
  }'

# Sync with custom credentials
curl -X POST https://api-id.execute-api.region.amazonaws.com/prod/sync \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{
    "email": "user@example.com",
    "password": "password",
    "totp_secret": "TOTPSECRET",
    "sync_type": "complete"
  }'
```

## Phase 6: Monitoring and Observability

### 6.1 CloudWatch Metrics

```go
// internal/events/cloudwatch_logger.go
package events

import (
    "context"
    "fmt"
    "os"
    
    "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
    "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type CloudWatchLogger struct {
    *Logger
    cwClient  *cloudwatch.Client
    namespace string
}

func NewCloudWatchLogger() *CloudWatchLogger {
    return &CloudWatchLogger{
        Logger:    NewLogger(InfoLevel),
        namespace: "Obsync/Lambda",
    }
}

func (l *CloudWatchLogger) RecordSyncMetrics(vaultID string, filesCount int, duration float64, success bool) {
    ctx := context.Background()
    
    dimensions := []types.Dimension{
        {
            Name:  aws.String("VaultID"),
            Value: aws.String(vaultID),
        },
        {
            Name:  aws.String("FunctionName"),
            Value: aws.String(os.Getenv("AWS_LAMBDA_FUNCTION_NAME")),
        },
    }
    
    metrics := []types.MetricDatum{
        {
            MetricName: aws.String("SyncDuration"),
            Value:      aws.Float64(duration),
            Unit:       types.StandardUnitSeconds,
            Dimensions: dimensions,
        },
        {
            MetricName: aws.String("FilesProcessed"),
            Value:      aws.Float64(float64(filesCount)),
            Unit:       types.StandardUnitCount,
            Dimensions: dimensions,
        },
        {
            MetricName: aws.String("SyncSuccess"),
            Value:      aws.Float64(boolToFloat(success)),
            Unit:       types.StandardUnitCount,
            Dimensions: dimensions,
        },
    }
    
    _, err := l.cwClient.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
        Namespace:  aws.String(l.namespace),
        MetricData: metrics,
    })
    
    if err != nil {
        l.WithError(err).Error("Failed to record metrics")
    }
}
```

## Phase 7: Error Handling and Resilience

### 7.1 Lambda-Specific Error Handling

```go
// internal/lambdahandler/errors.go
package lambdahandler

import (
    "context"
    "fmt"
    "time"
)

// RetryableError indicates an error that should trigger Lambda retry
type RetryableError struct {
    Err error
}

func (e RetryableError) Error() string {
    return fmt.Sprintf("retryable error: %v", e.Err)
}

// HandleLambdaErrors wraps errors appropriately for Lambda
func HandleLambdaErrors(err error) error {
    if err == nil {
        return nil
    }
    
    // Check for timeout
    if ctx.Err() == context.DeadlineExceeded {
        // Return success to prevent retry on timeout
        // State is saved incrementally
        return nil
    }
    
    // Check for retryable errors
    if isRetryable(err) {
        return RetryableError{Err: err}
    }
    
    // Non-retryable errors
    return err
}

func isRetryable(err error) bool {
    // Network errors, rate limits, etc.
    errStr := err.Error()
    retryablePatterns := []string{
        "connection refused",
        "rate limit",
        "throttled",
        "timeout",
        "temporary failure",
    }
    
    for _, pattern := range retryablePatterns {
        if strings.Contains(errStr, pattern) {
            return true
        }
    }
    
    return false
}
```

## Phase 8: Deployment Script

### 8.1 Makefile Additions

```makefile
# Lambda deployment targets
LAMBDA_FUNCTION_NAME ?= obsync-lambda
LAMBDA_REGION ?= us-east-1
LAMBDA_RUNTIME = provided.al2
LAMBDA_ARCH = arm64

.PHONY: build-lambda
build-lambda:
	GOOS=linux GOARCH=arm64 go build -tags lambda.norpc \
		-ldflags="-s -w" \
		-o bootstrap cmd/lambda/main.go
	zip obsync-lambda.zip bootstrap

.PHONY: deploy-lambda
deploy-lambda: build-lambda
	aws lambda update-function-code \
		--function-name $(LAMBDA_FUNCTION_NAME) \
		--zip-file fileb://obsync-lambda.zip \
		--region $(LAMBDA_REGION)

.PHONY: deploy-infrastructure
deploy-infrastructure:
	sam deploy \
		--template-file template.yaml \
		--stack-name obsync-lambda \
		--capabilities CAPABILITY_IAM \
		--parameter-overrides \
			ObsidianEmail=$(OBSIDIAN_EMAIL) \
			ObsidianPassword=$(OBSIDIAN_PASSWORD) \
			ObsidianTOTPSecret=$(OBSIDIAN_TOTP_SECRET)

.PHONY: test-lambda-local
test-lambda-local:
	sam local start-api

.PHONY: invoke-lambda
invoke-lambda:
	aws lambda invoke \
		--function-name $(LAMBDA_FUNCTION_NAME) \
		--payload '{"sync_type":"incremental"}' \
		--region $(LAMBDA_REGION) \
		response.json
	cat response.json
```

## Phase 9: Cost Optimization

### 9.1 Lambda Cost Optimizations

1. **Memory Optimization**: Start with 1024MB and adjust based on metrics
2. **Concurrent Execution Limits**: Set reserved concurrency to prevent runaway costs
3. **S3 Transfer Optimization**: Use S3 Transfer Acceleration for large files
4. **DynamoDB On-Demand**: Use on-demand billing for unpredictable workloads
5. **Lambda SnapStart**: Enable for faster cold starts (when available for Go)

### 9.2 Estimated Costs

```
Lambda Costs (per month):
- Invocations: 1M requests = $0.20
- Duration: 1M × 5s × 1GB = $83.33
- Total Lambda: ~$85/month

S3 Costs (per month):
- Storage: 100GB = $2.30
- Requests: 1M PUT + 2M GET = $5.50
- Transfer: 50GB = $4.50
- Total S3: ~$12/month

DynamoDB Costs (per month):
- On-demand reads/writes = ~$5/month

Total Estimated: ~$102/month for moderate usage
```

## Phase 10: Security Considerations

### 10.1 Security Best Practices

1. **Secrets Management**:
   - Use AWS Secrets Manager for credentials
   - Rotate credentials regularly
   - Never log sensitive data

2. **Network Security**:
   - Use VPC endpoints for S3/DynamoDB
   - Enable AWS WAF on API Gateway
   - Implement rate limiting

3. **Data Encryption**:
   - Enable S3 server-side encryption
   - Use DynamoDB encryption at rest
   - Maintain end-to-end encryption for vault data

4. **Access Control**:
   - Use IAM roles with least privilege
   - Enable CloudTrail for audit logging
   - Implement API key authentication

## Conclusion

This implementation plan provides a complete serverless architecture for running Obsync on AWS Lambda. The solution supports:

- ✅ Individual vault or all vaults synchronization
- ✅ Complete and incremental sync modes
- ✅ Scalable storage with S3
- ✅ Persistent state management with DynamoDB
- ✅ Scheduled and on-demand execution
- ✅ Comprehensive monitoring and logging
- ✅ Cost-effective serverless architecture

The architecture is designed to handle millions of sync operations while maintaining security, reliability, and cost efficiency.