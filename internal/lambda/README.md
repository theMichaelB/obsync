# Lambda Implementation

This directory contains the AWS Lambda implementation of Obsync, allowing serverless synchronization of Obsidian vaults.

## Architecture

The Lambda implementation provides:

- **Memory-aware processing** with automatic throttling when resources are low
- **Batch processing** for handling large vaults efficiently
- **Progress tracking** across Lambda invocations for partial syncs
- **Error recovery** for resuming failed syncs
- **S3 storage adapter** for vault file storage
- **DynamoDB state adapter** for sync state persistence

## Components

### Core Lambda Handler (`cmd/lambda/main.go`)
Entry point for Lambda function that processes sync events.

### Handler (`handler/`)
- **handler.go**: Main Lambda event processor
- **handler_test.go**: Basic tests for handler functionality

### Adapters (`adapters/`)
- **s3_store.go**: S3 implementation of BlobStore interface
- **dynamodb_store.go**: DynamoDB implementation of State Store interface

### Sync Components (`sync/`)
- **memory_manager.go**: Monitors and manages Lambda memory usage
- **batch_processor.go**: Processes files in batches with concurrency control
- **lambda_engine.go**: Lambda-optimized sync engine with timeout handling

### Progress Tracking (`progress/`)
- **tracker.go**: DynamoDB-based progress tracking for resumable syncs

### Error Recovery (`recovery/`)
- **recovery.go**: Handles sync recovery from partial failures

## Building

```bash
# Build Lambda deployment package
make build-lambda

# Test Lambda components
make test-lambda

# Clean Lambda build artifacts
make clean-lambda
```

## Environment Variables

Required:
- `OBSIDIAN_EMAIL`: Obsidian account email
- `OBSIDIAN_PASSWORD`: Obsidian account password  
- `OBSIDIAN_TOTP_SECRET`: TOTP secret for 2FA
- `S3_BUCKET`: S3 bucket for vault storage
- `STATE_TABLE_NAME`: DynamoDB table for state storage

Optional:
- `S3_PREFIX`: S3 key prefix for organization
- `LAMBDA_BATCH_SIZE`: Files per batch (default: 100)
- `LAMBDA_MAX_CONCURRENT`: Max concurrent batches (default: 5)

## Usage

The Lambda function accepts events with this structure:

```json
{
  "action": "sync",
  "vault_id": "optional-specific-vault",
  "sync_type": "incremental|complete",
  "dest_bucket": "optional-override-bucket",
  "dest_prefix": "optional-override-prefix"
}
```

Response format:

```json
{
  "success": true,
  "message": "Synced 2 vaults with 150 files",
  "vaults_synced": ["vault-1", "vault-2"],
  "files_count": 150,
  "errors": [],
  "metadata": {
    "sync_type": "incremental",
    "execution_time": "2m30s"
  }
}
```

## Memory Management

The Lambda implementation includes automatic memory management:

- Monitors memory usage every 5 seconds
- Pauses sync when memory usage exceeds 80%
- Resumes when usage drops below 60%
- Forces garbage collection when memory is low

## Timeout Handling

- Monitors Lambda execution time
- Stops processing 30 seconds before timeout
- Saves progress for resumption in next invocation
- Supports partial sync completion

## Error Recovery

- Tracks sync progress in DynamoDB
- Automatically resumes from last successful state
- Handles transient failures with exponential backoff
- Marks failed syncs as recoverable when appropriate