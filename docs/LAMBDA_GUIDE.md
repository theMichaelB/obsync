# Obsync AWS Lambda Implementation Guide

## Overview

Obsync supports serverless deployment on AWS Lambda, enabling automated vault synchronization without maintaining persistent infrastructure. The Lambda implementation provides the same functionality as the CLI version but optimized for serverless constraints and cloud storage.

## Architecture

### Lambda vs CLI Comparison

| Component | CLI Mode | Lambda Mode |
|-----------|----------|-------------|
| **Storage** | Local filesystem (`LocalStore`) | Amazon S3 (`S3Store`) with vault-specific prefixes |
| **State Management** | SQLite files (`JSONStore`) | S3 with versioning (`S3StateStore`) and conflict resolution |
| **Authentication** | Local token files | Temporary `/tmp` storage |
| **Memory Management** | OS managed | Custom `MemoryManager` with throttling |
| **Concurrency** | User configurable | Lambda-optimized (typically 3) |
| **Timeout Handling** | Unlimited | 15-minute Lambda limit with 30s buffer |

### Key Components

#### 1. Lambda Handler (`internal/lambda/handler/`)
The main entry point that processes Lambda events and coordinates sync operations.

#### 2. S3 Adapters (`internal/lambda/adapters/`)
- **S3Store**: Handles vault file storage in S3 with vault-specific prefixes
- **S3StateStore**: Manages sync state with S3 versioning and conflict resolution

#### 3. Memory Management (`internal/lambda/sync/`)
- **MemoryManager**: Monitors and controls memory usage during sync operations
- **Automatic throttling** when memory exceeds 80% of available Lambda memory

#### 4. Progress Tracking (`internal/lambda/progress/`)
- **ProgressTracker**: Provides real-time sync progress updates
- **Resumable operations** with checkpoint persistence

## Configuration

### Environment Variables

#### Required AWS Configuration
```bash
AWS_LAMBDA_FUNCTION_NAME=obsync-sync-function  # Triggers Lambda mode
S3_BUCKET=your-obsync-bucket                   # S3 bucket for storage
AWS_REGION=us-east-1                           # AWS region
```

#### Required Authentication
```bash
OBSYNC_SECRET_NAME=obsync-credentials         # AWS Secrets Manager secret name/ARN
```

The secret should contain combined credentials in JSON format:
```json
{
  "auth": {
    "email": "your-email@example.com",
    "password": "your-secure-password",
    "totp_secret": "your-totp-secret"  # Base32 encoded
  },
  "vaults": {
    "My Vault": {"password": "vault-specific-password"}
  }
}
```

#### Optional Configuration
```bash
S3_PREFIX=vaults/                             # S3 key prefix for vault files
S3_STATE_PREFIX=state/                        # S3 key prefix for state files
DOWNLOAD_ON_STARTUP=true                      # Pre-download state files for performance
CACHE_TIMEOUT=300                             # Local state cache timeout (seconds)
LAMBDA_BATCH_SIZE=50                          # Files per batch (default: 50)
LAMBDA_MAX_CONCURRENT=3                       # Concurrent operations (default: 3)
LAMBDA_TIMEOUT_BUFFER=30                      # Seconds before Lambda timeout
```

### IAM Permissions

The Lambda function requires the following IAM permissions:

```json
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
                "arn:aws:s3:::your-obsync-bucket",
                "arn:aws:s3:::your-obsync-bucket/*"
            ]
        }
    ]
}
```

## Implementation Details

### Lambda Mode Detection

Lambda mode is automatically detected using environment variables:

```go
// internal/config/lambda.go
func IsLambdaEnvironment() bool {
    return os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
}
```

### Client Initialization

The Lambda client uses different storage and state adapters:

```go
// internal/client/lambda_extensions.go
func NewLambdaClient(cfg *config.Config, logger *events.Logger) (*Client, error) {
    logger.Info("Initializing S3 storage client")
    
    // Note: Can be used for both Lambda and CLI S3 mode
    
    // Create S3 storage with vault-specific prefixes
    s3Store, err := adapters.NewS3Store(
        lambdaCfg.S3Bucket, 
        lambdaCfg.S3Prefix, 
        logger,
    )
    
    // Create S3 state store with versioning
    s3StateStore, err := adapters.NewS3StateStore(
        lambdaCfg.S3Bucket,
        lambdaCfg.S3StatePrefix,
        logger,
    )
    
    // Download existing states for performance
    if lambdaCfg.DownloadOnStartup {
        s3StateStore.DownloadStatesOnStartup(ctx)
    }
    
    return client, nil
}
```

### S3 Storage Implementation

#### Vault-Specific Prefixes
Each vault gets its own directory structure in S3:

```go
// internal/lambda/adapters/s3_store.go
func (s *S3Store) SetBasePath(basePath string) error {
    vaultName := filepath.Base(basePath)
    originalPrefix := strings.TrimSuffix(s.prefix, "/")
    
    if originalPrefix != "" {
        s.prefix = originalPrefix + "/" + vaultName + "/"
    } else {
        s.prefix = vaultName + "/"
    }
    
    return nil
}
```

**Resulting S3 structure:**
```
s3://your-bucket/
├── vaults/
│   ├── vault-name-1/
│   │   ├── .obsidian/
│   │   ├── notes/
│   │   └── attachments/
│   ├── vault-name-2/
│   │   └── ...
└── state/
    ├── vault-id-1.json
    └── vault-id-2.json
```

#### S3 State Management with Versioning

State files use S3 object versioning for conflict resolution and consistency:

```go
// internal/lambda/adapters/s3_state_store.go
func (s *S3StateStore) Save(vaultID string, syncState *models.SyncState) error {
    // Use conditional write with ETag for consistency
    if ifMatch != nil {
        putInput.IfMatch = ifMatch
    }
    
    result, err := s.client.PutObject(ctx, putInput)
    if err != nil {
        // Handle conflicts by retrying
        if strings.Contains(err.Error(), "PreconditionFailed") {
            delete(s.localCache, vaultID)
            return s.Save(vaultID, syncState)
        }
    }
    
    // Cache the new version ID
    s.localCache[vaultID] = cacheEntry{
        state:     syncState,
        versionID: aws.ToString(result.VersionId),
        timestamp: time.Now(),
    }
    
    return nil
}
```

#### Local Caching and Performance

The S3StateStore includes local caching to reduce API calls:

```go
type cacheEntry struct {
    state     *models.SyncState
    versionID string
    timestamp time.Time
}

// Check cache before S3 operation
func (s *S3StateStore) Load(vaultID string) (*models.SyncState, error) {
    // Check local cache first
    if entry, exists := s.localCache[vaultID]; exists {
        if time.Since(entry.timestamp) < s.cacheTimeout {
            return entry.state, nil
        }
    }
    
    // Fall back to S3 if cache miss/expired
    return s.loadFromS3(vaultID)
}
```

### Recent S3 Storage Improvements

#### 1. Vault-Specific Prefixes (Fixed Organization Issue)

**Problem**: Previously, all vaults synced to a flat `vaults/` structure, causing file conflicts.

**Solution**: Implemented `SetBasePath()` method to create vault-specific prefixes:

```go
// Before: vaults/file.md (all vaults mixed together)
// After:  vaults/Vault-Name/file.md (organized by vault)

func (s *S3Store) SetBasePath(basePath string) error {
    vaultName := filepath.Base(basePath)
    originalPrefix := strings.TrimSuffix(s.prefix, "/")
    
    if originalPrefix != "" {
        s.prefix = originalPrefix + "/" + vaultName + "/"
    } else {
        s.prefix = vaultName + "/"
    }
    return nil
}
```

#### 2. Enhanced Client Compatibility

**Problem**: CLI couldn't use S3 storage for testing Lambda functionality.

**Solution**: Extended `Client.SetStorageBase()` to support S3Store:

```go
func (c *Client) SetStorageBase(basePath string) error {
    if localStore, ok := c.storage.(*storage.LocalStore); ok {
        return localStore.SetBasePath(basePath)
    }
    
    // Handle S3Store for Lambda mode
    if s3Store, ok := c.storage.(interface{ SetBasePath(string) error }); ok {
        return s3Store.SetBasePath(basePath)
    }
    
    return nil
}
```

#### 3. Hybrid CLI/Lambda Mode

**Enhancement**: `NewLambdaClient()` now works for both Lambda and CLI S3 testing:

```go
func NewLambdaClient(cfg *config.Config, logger *events.Logger) (*Client, error) {
    logger.Info("Initializing S3 storage client")
    
    // Works in both Lambda environment and CLI with --s3 flag
    // Different token storage paths for each environment
    var tokenFile string
    if config.IsLambdaEnvironment() {
        tokenFile = filepath.Join("/tmp/obsync/auth", "token.json")
    } else {
        // CLI S3 mode uses configured token path
        tokenFile = cfg.Auth.TokenFile
    }
    
    return client, nil
}
```

### Memory Management

Lambda functions have limited memory, so the system includes active memory monitoring:

```go
// internal/lambda/sync/memory_manager.go
func (m *MemoryManager) CheckMemory() bool {
    var memStats runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&memStats)
    
    usedMB := float64(memStats.Alloc) / 1024 / 1024
    usagePercent := usedMB / float64(m.totalMemoryMB) * 100
    
    if usagePercent > 80 {
        m.logger.WithField("usage_percent", usagePercent).
            Warn("High memory usage, pausing processing")
        return false
    }
    
    return true
}
```

### Timeout Handling

Lambda functions have a maximum execution time. The system reserves a buffer before timeout:

```go
// Detect remaining Lambda execution time
remaining := context.DeadlineFromContext(ctx)
if remaining < 30*time.Second {
    logger.Warn("Approaching Lambda timeout, saving progress")
    return s.saveProgress(ctx)
}
```

## Usage

### Building Lambda Package

```bash
# Build optimized Lambda binary
make build-lambda

# This creates build/obsync-lambda.zip ready for deployment
```

### Testing S3 Storage Mode

You can test S3 storage functionality locally without Lambda:

```bash
# Set S3 environment variables
export S3_BUCKET="your-test-bucket"
export S3_PREFIX="vaults/"
export S3_STATE_PREFIX="state/"
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"

# Create credentials file
cp credentials.example.json credentials.json
# Edit credentials.json with your account and vault passwords

# Create config file
echo '{
  "auth": {
    "combined_credentials_file": "./credentials.json"
  }
}' > config.json

# Run sync with S3 storage (--s3 flag enables S3 mode)
./obsync --s3 sync <vault-id> --dest ./vault-name
```

### Lambda Environment Testing

For full Lambda mode testing:

```bash
# Enable Lambda mode locally
export AWS_LAMBDA_FUNCTION_NAME="test-mode"
# ... (add all the S3 and auth variables above)

# Run sync (will use Lambda-optimized components)
./obsync sync <vault-id> --dest /tmp/vault-name --password <vault-password>
```

### Lambda Event Formats

The Lambda handler accepts various event formats:

#### Single Vault Sync
```json
{
    "type": "sync_vault",
    "vault_id": "abc123...",
    "vault_password": "encrypted_password",
    "options": {
        "full_sync": false,
        "timeout_minutes": 10
    }
}
```

#### Multiple Vault Sync
```json
{
    "type": "sync_multiple",
    "vaults": [
        {
            "vault_id": "abc123...",
            "vault_password": "password1"
        },
        {
            "vault_id": "def456...",
            "vault_password": "password2"
        }
    ],
    "options": {
        "max_concurrent": 2,
        "full_sync": true
    }
}
```

#### Status Check
```json
{
    "type": "status",
    "vault_ids": ["abc123...", "def456..."]
}
```

### Deployment Options

#### AWS SAM Template
```yaml
AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31

Resources:
  ObsyncFunction:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: build/obsync-lambda.zip
      Handler: bootstrap
      Runtime: provided.al2
      Architecture: arm64
      MemorySize: 512
      Timeout: 900
      Environment:
        Variables:
          S3_BUCKET: !Ref StorageBucket
          S3_PREFIX: "vaults/"
          S3_STATE_PREFIX: "state/"
          OBSYNC_SECRET_NAME: !Ref ObsyncSecretArn
      Events:
        ScheduledSync:
          Type: Schedule
          Properties:
            Schedule: rate(1 hour)
  
  StorageBucket:
    Type: AWS::S3::Bucket
    Properties:
      VersioningConfiguration:
        Status: Enabled
```

#### Terraform Example
```hcl
resource "aws_lambda_function" "obsync" {
  filename         = "build/obsync-lambda.zip"
  function_name    = "obsync-sync"
  role            = aws_iam_role.lambda_role.arn
  handler         = "bootstrap"
  runtime         = "provided.al2"
  architectures   = ["arm64"]
  memory_size     = 512
  timeout         = 900

  environment {
    variables = {
      S3_BUCKET             = aws_s3_bucket.obsync.bucket
      S3_PREFIX            = "vaults/"
      S3_STATE_PREFIX      = "state/"
      OBSYNC_SECRET_NAME   = aws_secretsmanager_secret.obsync_credentials.arn
    }
  }
}

resource "aws_s3_bucket" "obsync" {
  bucket = "obsync-${random_id.bucket_suffix.hex}"
}

resource "aws_s3_bucket_versioning" "obsync" {
  bucket = aws_s3_bucket.obsync.id
  versioning_configuration {
    status = "Enabled"
  }
}
```

### Monitoring and Observability

#### CloudWatch Logs
Lambda execution logs are automatically sent to CloudWatch:

```
2024-01-15 10:30:00 [INFO] Initializing Lambda-optimized client
2024-01-15 10:30:01 [INFO] Downloading states on startup component=s3_state_store
2024-01-15 10:30:02 [INFO] Starting sync vault_id=abc123... initial=false
2024-01-15 10:30:10 [INFO] Sync completed successfully files=145 duration=8.2s
```

#### Custom Metrics
The system can emit custom CloudWatch metrics:

```go
// Emit custom metrics
cloudwatch.PutMetric("Obsync/SyncDuration", duration, "Seconds")
cloudwatch.PutMetric("Obsync/FilesProcessed", fileCount, "Count")
cloudwatch.PutMetric("Obsync/MemoryUsage", memoryMB, "Megabytes")
```

## Performance Optimization

### Cold Start Mitigation
- **State caching**: Download all vault states on startup
- **Connection pooling**: Reuse WebSocket connections
- **Memory preallocation**: Allocate buffers upfront

### Batch Processing
```go
// Process files in batches to manage memory
batchSize := cfg.BatchSize // default: 50
for i := 0; i < len(files); i += batchSize {
    end := min(i+batchSize, len(files))
    batch := files[i:end]
    
    if !memManager.CheckMemory() {
        // Wait for memory to free up
        runtime.GC()
        time.Sleep(1 * time.Second)
    }
    
    processBatch(batch)
}
```

### Concurrency Control
```go
// Limit concurrent operations based on Lambda memory
maxConcurrent := calculateOptimalConcurrency(lambdaMemoryMB)
sem := make(chan struct{}, maxConcurrent)

for _, file := range files {
    sem <- struct{}{}
    go func(f File) {
        defer func() { <-sem }()
        processFile(f)
    }(file)
}
```

## Error Handling and Recovery

### Automatic Retry Logic
```go
func (s *SyncEngine) syncWithRetry(ctx context.Context, vaultID string) error {
    const maxRetries = 3
    
    for attempt := 1; attempt <= maxRetries; attempt++ {
        err := s.sync(ctx, vaultID)
        if err == nil {
            return nil
        }
        
        if isRetryableError(err) && attempt < maxRetries {
            backoff := time.Duration(attempt) * time.Second
            time.Sleep(backoff)
            continue
        }
        
        return err
    }
    
    return fmt.Errorf("max retries exceeded")
}
```

### Graceful Degradation
- **Partial sync completion**: Save progress before timeout
- **State persistence**: Always save intermediate states
- **Connection recovery**: Automatic WebSocket reconnection

## Security Considerations

### Credential Management
- **Never log credentials**: Sanitize all log output
- **Environment variables only**: No hardcoded secrets
- **Temporary storage**: Use `/tmp` for ephemeral data only

### Network Security
- **HTTPS only**: All API communications use TLS
- **WebSocket security**: Authenticated WebSocket connections
- **VPC integration**: Optional VPC deployment for network isolation

### Data Encryption
- **Transit encryption**: TLS 1.3 for all network communications
- **Storage encryption**: S3 server-side encryption enabled
- **Vault encryption**: Obsidian's native AES-256-GCM encryption maintained

## Troubleshooting

### Common Issues

#### 1. Memory Errors
```
Error: Lambda function exceeded memory limit
```
**Solution**: Increase Lambda memory or reduce `LAMBDA_BATCH_SIZE`

#### 2. Timeout Issues
```
Error: Lambda function timeout
```
**Solution**: 
- Increase Lambda timeout (max 15 minutes)
- Reduce `LAMBDA_MAX_CONCURRENT`
- Enable `LAMBDA_DOWNLOAD_ON_STARTUP=false` for faster startup

#### 3. S3 Permission Errors
```
Error: Access Denied for S3 operation
```
**Solution**: Verify IAM role has required S3 permissions

#### 4. Authentication Failures
```
Error: invalid login response
```
**Solution**: 
- Verify TOTP secret is correct and base32 encoded
- Check system clock synchronization
- Ensure credentials are not expired

### Debug Mode
Enable detailed logging with:
```bash
export LOG_LEVEL=debug
```

## Best Practices

### Lambda Configuration
- **Memory**: Start with 512MB, increase if needed
- **Timeout**: Set to 900s (15 minutes) for large vaults
- **Architecture**: Use ARM64 for better price/performance

### S3 Configuration  
- **Versioning**: Always enable for state conflict resolution
- **Lifecycle policies**: Archive old versions after 30 days
- **Cross-region replication**: Consider for disaster recovery

### Monitoring
- **CloudWatch alarms**: Set up alerts for failures and high duration
- **Cost monitoring**: Track S3 storage and Lambda execution costs
- **Performance baselines**: Establish normal sync duration patterns

### Scaling
- **Multiple functions**: Deploy separate functions for different vault groups
- **Event-driven**: Use S3/CloudWatch events to trigger syncs
- **Regional deployment**: Deploy in multiple regions for global access

This comprehensive implementation provides enterprise-grade serverless vault synchronization with proper error handling, monitoring, and scalability considerations.