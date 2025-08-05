# Lambda Implementation Details

## Additional Components

### 9. Memory-Efficient Sync Engine

Since Lambda has limited memory and /tmp space, we need to optimize the sync engine:

```go
// internal/lambda/sync/memory_manager.go
package sync

import (
    "fmt"
    "os"
    "runtime"
    "sync"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/events"
)

type MemoryManager struct {
    logger        *events.Logger
    maxMemoryMB   int64
    maxTmpSizeMB  int64
    checkInterval time.Duration
    mu            sync.RWMutex
    paused        bool
}

func NewMemoryManager(logger *events.Logger) *MemoryManager {
    return &MemoryManager{
        logger:        logger,
        maxMemoryMB:   getMaxMemoryMB(),
        maxTmpSizeMB:  400, // Leave 100MB buffer in /tmp
        checkInterval: 5 * time.Second,
    }
}

func (m *MemoryManager) Start() {
    go m.monitor()
}

func (m *MemoryManager) monitor() {
    ticker := time.NewTicker(m.checkInterval)
    defer ticker.Stop()
    
    for range ticker.C {
        m.checkResources()
    }
}

func (m *MemoryManager) checkResources() {
    // Check memory usage
    var memStats runtime.MemStats
    runtime.ReadMemStats(&memStats)
    
    usedMB := int64(memStats.Alloc / 1024 / 1024)
    percentUsed := float64(usedMB) / float64(m.maxMemoryMB) * 100
    
    // Check /tmp usage
    tmpUsedMB := m.getTmpUsage()
    tmpPercentUsed := float64(tmpUsedMB) / float64(m.maxTmpSizeMB) * 100
    
    m.logger.WithFields(map[string]interface{}{
        "memory_used_mb":     usedMB,
        "memory_percent":     percentUsed,
        "tmp_used_mb":        tmpUsedMB,
        "tmp_percent":        tmpPercentUsed,
    }).Debug("Resource usage")
    
    // Pause sync if resources are low
    m.mu.Lock()
    if percentUsed > 80 || tmpPercentUsed > 80 {
        if !m.paused {
            m.paused = true
            m.logger.Warn("Pausing sync due to resource constraints")
            runtime.GC() // Force garbage collection
        }
    } else if m.paused && percentUsed < 60 && tmpPercentUsed < 60 {
        m.paused = false
        m.logger.Info("Resuming sync after resource recovery")
    }
    m.mu.Unlock()
}

func (m *MemoryManager) ShouldPause() bool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.paused
}

func (m *MemoryManager) getTmpUsage() int64 {
    var stat os.FileInfo
    var err error
    
    stat, err = os.Stat("/tmp")
    if err != nil {
        return 0
    }
    
    // Note: This is simplified - in production, use syscall.Statfs
    return stat.Size() / 1024 / 1024
}

func getMaxMemoryMB() int64 {
    // Get from Lambda environment
    memSize := os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE")
    if memSize == "" {
        return 1024 // Default 1GB
    }
    
    var size int64
    fmt.Sscanf(memSize, "%d", &size)
    return size
}
```

### 10. Batch Processing for Large Vaults

Handle large vaults by processing in batches:

```go
// internal/lambda/sync/batch_processor.go
package sync

import (
    "context"
    "fmt"
    "sync"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/models"
)

type BatchProcessor struct {
    batchSize     int
    maxConcurrent int
    memManager    *MemoryManager
}

func NewBatchProcessor(memManager *MemoryManager) *BatchProcessor {
    return &BatchProcessor{
        batchSize:     100,  // Files per batch
        maxConcurrent: 5,    // Concurrent batches
        memManager:    memManager,
    }
}

func (p *BatchProcessor) ProcessFiles(ctx context.Context, files []models.FileInfo, 
    processFunc func(context.Context, []models.FileInfo) error) error {
    
    // Split into batches
    batches := p.splitIntoBatches(files)
    
    // Process batches with concurrency control
    sem := make(chan struct{}, p.maxConcurrent)
    errChan := make(chan error, len(batches))
    var wg sync.WaitGroup
    
    for i, batch := range batches {
        // Check if we should pause for memory
        for p.memManager.ShouldPause() {
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(1 * time.Second):
                continue
            }
        }
        
        wg.Add(1)
        sem <- struct{}{}
        
        go func(batchNum int, files []models.FileInfo) {
            defer wg.Done()
            defer func() { <-sem }()
            
            if err := processFunc(ctx, files); err != nil {
                errChan <- fmt.Errorf("batch %d: %w", batchNum, err)
            }
        }(i, batch)
    }
    
    wg.Wait()
    close(errChan)
    
    // Collect errors
    var errs []error
    for err := range errChan {
        errs = append(errs, err)
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("batch processing failed with %d errors: %v", len(errs), errs[0])
    }
    
    return nil
}

func (p *BatchProcessor) splitIntoBatches(files []models.FileInfo) [][]models.FileInfo {
    var batches [][]models.FileInfo
    
    for i := 0; i < len(files); i += p.batchSize {
        end := i + p.batchSize
        if end > len(files) {
            end = len(files)
        }
        batches = append(batches, files[i:end])
    }
    
    return batches
}
```

### 11. Lambda-Optimized Sync Engine

Extend the sync engine for Lambda constraints:

```go
// internal/lambda/sync/lambda_engine.go
package sync

import (
    "context"
    "fmt"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/crypto"
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/models"
    "github.com/TheMichaelB/obsync/internal/services/sync"
    "github.com/TheMichaelB/obsync/internal/state"
    "github.com/TheMichaelB/obsync/internal/storage"
    "github.com/TheMichaelB/obsync/internal/transport"
)

type LambdaEngine struct {
    *sync.Engine
    memManager     *MemoryManager
    batchProcessor *BatchProcessor
    startTime      time.Time
    timeoutBuffer  time.Duration
}

func NewLambdaEngine(
    transport transport.Transport,
    crypto crypto.Provider,
    state state.Store,
    storage storage.BlobStore,
    logger *events.Logger,
) *LambdaEngine {
    memManager := NewMemoryManager(logger)
    memManager.Start()
    
    return &LambdaEngine{
        Engine:         sync.NewEngine(transport, crypto, state, storage, logger),
        memManager:     memManager,
        batchProcessor: NewBatchProcessor(memManager),
        startTime:      time.Now(),
        timeoutBuffer:  30 * time.Second, // Stop 30s before Lambda timeout
    }
}

func (e *LambdaEngine) Sync(ctx context.Context, vaultID, vaultHost string, 
    vaultKey []byte, encryptionVersion int, initial bool) error {
    
    // Create timeout context based on Lambda remaining time
    timeoutCtx, cancel := e.createTimeoutContext(ctx)
    defer cancel()
    
    // Override base sync to add Lambda optimizations
    return e.syncWithOptimizations(timeoutCtx, vaultID, vaultHost, vaultKey, 
        encryptionVersion, initial)
}

func (e *LambdaEngine) syncWithOptimizations(ctx context.Context, vaultID, vaultHost string,
    vaultKey []byte, encryptionVersion int, initial bool) error {
    
    e.logger.WithFields(map[string]interface{}{
        "vault_id": vaultID,
        "initial":  initial,
        "lambda_memory": getMaxMemoryMB(),
    }).Info("Starting Lambda-optimized sync")
    
    // For large initial syncs, use incremental approach
    if initial {
        return e.incrementalInitialSync(ctx, vaultID, vaultHost, vaultKey, encryptionVersion)
    }
    
    // Regular incremental sync
    return e.Engine.Sync(ctx, vaultID, vaultHost, vaultKey, encryptionVersion, initial)
}

func (e *LambdaEngine) incrementalInitialSync(ctx context.Context, vaultID, vaultHost string,
    vaultKey []byte, encryptionVersion int) error {
    
    // Get vault state
    syncState, err := e.State.Load(vaultID)
    if err != nil {
        return fmt.Errorf("load state: %w", err)
    }
    
    // Connect to WebSocket
    initMsg := models.InitMessage{
        ID:                vaultID,
        Initial:           true,
        Version:           syncState.Version,
        EncryptionVersion: encryptionVersion,
    }
    
    msgChan, err := e.Transport.StreamWS(ctx, vaultHost, initMsg)
    if err != nil {
        return fmt.Errorf("stream websocket: %w", err)
    }
    
    // Process messages in batches
    var fileBuffer []models.FileInfo
    
    for msg := range msgChan {
        // Check timeout
        if e.shouldTimeout() {
            e.logger.Warn("Approaching Lambda timeout, saving progress")
            break
        }
        
        switch msg.Type {
        case models.WSTypeFile:
            var fileMsg models.FileMessage
            if err := msg.ParseData(&fileMsg); err != nil {
                continue
            }
            
            fileBuffer = append(fileBuffer, models.FileInfo{
                Path: fileMsg.Path,
                Hash: fileMsg.Hash,
                Size: int64(fileMsg.Size),
            })
            
            // Process batch when buffer is full
            if len(fileBuffer) >= e.batchProcessor.batchSize {
                if err := e.processBatch(ctx, vaultID, vaultKey, fileBuffer); err != nil {
                    return err
                }
                fileBuffer = nil
            }
            
        case models.WSTypeDone:
            // Process remaining files
            if len(fileBuffer) > 0 {
                if err := e.processBatch(ctx, vaultID, vaultKey, fileBuffer); err != nil {
                    return err
                }
            }
            
            // Update state
            var doneMsg models.DoneMessage
            if err := msg.ParseData(&doneMsg); err == nil {
                syncState.Version = doneMsg.FinalVersion
                syncState.LastSync = time.Now()
                
                if err := e.State.Save(vaultID, syncState); err != nil {
                    return fmt.Errorf("save state: %w", err)
                }
            }
            
            return nil
        }
    }
    
    // Timeout occurred - save progress
    if len(fileBuffer) > 0 {
        if err := e.processBatch(ctx, vaultID, vaultKey, fileBuffer); err != nil {
            return err
        }
    }
    
    return fmt.Errorf("sync incomplete due to timeout")
}

func (e *LambdaEngine) processBatch(ctx context.Context, vaultID string, 
    vaultKey []byte, files []models.FileInfo) error {
    
    return e.batchProcessor.ProcessFiles(ctx, files, 
        func(ctx context.Context, batch []models.FileInfo) error {
            for _, file := range batch {
                if err := e.downloadAndStoreFile(ctx, vaultID, vaultKey, file); err != nil {
                    e.logger.WithError(err).WithField("path", file.Path).
                        Warn("Failed to process file")
                    continue
                }
            }
            return nil
        })
}

func (e *LambdaEngine) createTimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
    // Get remaining time from Lambda context
    deadline, ok := ctx.Deadline()
    if !ok {
        // Not in Lambda or no deadline set
        return context.WithTimeout(ctx, 14*time.Minute)
    }
    
    // Set timeout with buffer
    remaining := time.Until(deadline) - e.timeoutBuffer
    if remaining < 0 {
        remaining = 1 * time.Second
    }
    
    return context.WithTimeout(ctx, remaining)
}

func (e *LambdaEngine) shouldTimeout() bool {
    elapsed := time.Since(e.startTime)
    return elapsed > (14*time.Minute + 30*time.Second) // 14.5 minutes
}
```

### 12. Progress Tracking for Lambda

Track progress across Lambda invocations:

```go
// internal/lambda/progress/tracker.go
package progress

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type ProgressTracker struct {
    dynamoClient *dynamodb.Client
    tableName    string
}

type SyncProgress struct {
    VaultID       string    `json:"vault_id"`
    StartTime     time.Time `json:"start_time"`
    LastUpdate    time.Time `json:"last_update"`
    FilesTotal    int       `json:"files_total"`
    FilesComplete int       `json:"files_complete"`
    BytesTotal    int64     `json:"bytes_total"`
    BytesComplete int64     `json:"bytes_complete"`
    Status        string    `json:"status"` // "running", "complete", "failed"
    Error         string    `json:"error,omitempty"`
    Resumable     bool      `json:"resumable"`
}

func (t *ProgressTracker) SaveProgress(ctx context.Context, progress SyncProgress) error {
    progress.LastUpdate = time.Now()
    
    progressJSON, err := json.Marshal(progress)
    if err != nil {
        return fmt.Errorf("marshal progress: %w", err)
    }
    
    _, err = t.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: &t.tableName,
        Item: map[string]types.AttributeValue{
            "vault_id": &types.AttributeValueMemberS{
                Value: fmt.Sprintf("progress#%s", progress.VaultID),
            },
            "data": &types.AttributeValueMemberS{
                Value: string(progressJSON),
            },
            "ttl": &types.AttributeValueMemberN{
                Value: fmt.Sprintf("%d", time.Now().Add(24*time.Hour).Unix()),
            },
        },
    })
    
    return err
}

func (t *ProgressTracker) GetProgress(ctx context.Context, vaultID string) (*SyncProgress, error) {
    result, err := t.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
        TableName: &t.tableName,
        Key: map[string]types.AttributeValue{
            "vault_id": &types.AttributeValueMemberS{
                Value: fmt.Sprintf("progress#%s", vaultID),
            },
        },
    })
    
    if err != nil {
        return nil, err
    }
    
    if result.Item == nil {
        return nil, nil
    }
    
    dataAttr := result.Item["data"].(*types.AttributeValueMemberS)
    var progress SyncProgress
    if err := json.Unmarshal([]byte(dataAttr.Value), &progress); err != nil {
        return nil, err
    }
    
    return &progress, nil
}
```

### 13. Lambda Configuration Updates

Add Lambda-specific configuration options:

```go
// internal/config/lambda.go
package config

import (
    "os"
    "strconv"
    "time"
)

// LambdaConfig contains Lambda-specific settings
type LambdaConfig struct {
    MaxMemoryMB      int           `json:"max_memory_mb"`
    TimeoutBuffer    time.Duration `json:"timeout_buffer"`
    BatchSize        int           `json:"batch_size"`
    MaxConcurrent    int           `json:"max_concurrent"`
    EnableProgress   bool          `json:"enable_progress"`
    S3Bucket         string        `json:"s3_bucket"`
    S3Prefix         string        `json:"s3_prefix"`
    StateTableName   string        `json:"state_table_name"`
}

// LoadLambdaConfig loads configuration for Lambda environment
func LoadLambdaConfig() *LambdaConfig {
    cfg := &LambdaConfig{
        MaxMemoryMB:   1024,
        TimeoutBuffer: 30 * time.Second,
        BatchSize:     100,
        MaxConcurrent: 5,
        EnableProgress: true,
    }
    
    // Override from environment
    if v := os.Getenv("LAMBDA_MAX_MEMORY_MB"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            cfg.MaxMemoryMB = n
        }
    }
    
    if v := os.Getenv("LAMBDA_BATCH_SIZE"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            cfg.BatchSize = n
        }
    }
    
    if v := os.Getenv("LAMBDA_MAX_CONCURRENT"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            cfg.MaxConcurrent = n
        }
    }
    
    cfg.S3Bucket = os.Getenv("S3_BUCKET")
    cfg.S3Prefix = os.Getenv("S3_PREFIX")
    cfg.StateTableName = os.Getenv("STATE_TABLE_NAME")
    
    if cfg.StateTableName == "" {
        cfg.StateTableName = "obsync-state"
    }
    
    return cfg
}
```

### 14. Error Recovery

Add error recovery for partial syncs:

```go
// internal/lambda/recovery/recovery.go
package recovery

import (
    "context"
    "fmt"
    "time"
    
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/models"
)

type RecoveryManager struct {
    logger   *events.Logger
    progress *ProgressTracker
}

func (r *RecoveryManager) RecoverSync(ctx context.Context, vaultID string) (*models.SyncState, error) {
    // Check for previous progress
    progress, err := r.progress.GetProgress(ctx, vaultID)
    if err != nil {
        return nil, fmt.Errorf("get progress: %w", err)
    }
    
    if progress == nil {
        r.logger.WithField("vault_id", vaultID).Info("No previous progress found")
        return models.NewSyncState(), nil
    }
    
    // Check if recovery is viable
    if !progress.Resumable {
        r.logger.WithField("vault_id", vaultID).Warn("Previous sync not resumable")
        return models.NewSyncState(), nil
    }
    
    // Check age of progress
    age := time.Since(progress.LastUpdate)
    if age > 1*time.Hour {
        r.logger.WithFields(map[string]interface{}{
            "vault_id": vaultID,
            "age":      age,
        }).Warn("Progress too old, starting fresh")
        return models.NewSyncState(), nil
    }
    
    r.logger.WithFields(map[string]interface{}{
        "vault_id":       vaultID,
        "files_complete": progress.FilesComplete,
        "files_total":    progress.FilesTotal,
    }).Info("Recovering previous sync")
    
    // Return state that can resume
    return &models.SyncState{
        Version:      progress.LastVersion,
        LastSync:     progress.StartTime,
        FileCount:    progress.FilesComplete,
        TotalSize:    progress.BytesComplete,
        ResumeToken:  progress.ResumeToken,
    }, nil
}
```

### 15. Integration with Existing Client

Update the client to support Lambda mode:

```go
// internal/client/lambda_extensions.go
package client

import (
    "os"
    
    "github.com/TheMichaelB/obsync/internal/config"
    "github.com/TheMichaelB/obsync/internal/events"
    "github.com/TheMichaelB/obsync/internal/lambda/adapters"
    "github.com/TheMichaelB/obsync/internal/lambda/sync"
)

// NewLambdaClient creates a client optimized for Lambda
func NewLambdaClient(cfg *config.Config, logger *events.Logger) (*Client, error) {
    // Detect if running in Lambda
    if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == "" {
        return New(cfg, logger)
    }
    
    logger.Info("Initializing Lambda-optimized client")
    
    // Create Lambda-specific components
    lambdaCfg := config.LoadLambdaConfig()
    
    // Create S3 storage
    s3Store, err := adapters.NewS3Store(
        lambdaCfg.S3Bucket, 
        lambdaCfg.S3Prefix, 
        logger,
    )
    if err != nil {
        return nil, fmt.Errorf("create s3 store: %w", err)
    }
    
    // Create DynamoDB state
    dynamoStore, err := adapters.NewDynamoDBStore(
        lambdaCfg.StateTableName,
        logger,
    )
    if err != nil {
        return nil, fmt.Errorf("create dynamodb store: %w", err)
    }
    
    // Create Lambda-optimized sync engine
    lambdaEngine := sync.NewLambdaEngine(
        transport,
        crypto,
        dynamoStore,
        s3Store,
        logger,
    )
    
    // Return client with Lambda components
    return &Client{
        // ... configured with Lambda adapters
    }, nil
}
```

## Testing Lambda Implementation

### Unit Tests

```go
// internal/lambda/handler/handler_test.go
func TestLambdaHandler(t *testing.T) {
    t.Run("handles memory pressure", func(t *testing.T) {
        // Test memory manager pausing
    })
    
    t.Run("respects timeout", func(t *testing.T) {
        // Test timeout handling
    })
    
    t.Run("recovers from partial sync", func(t *testing.T) {
        // Test recovery mechanism
    })
}
```

### Local Testing

```bash
# Test Lambda handler locally
sam local start-lambda

# Invoke with test event
sam local invoke ObsyncFunction -e events/sync-single-vault.json

# Test with Docker
docker run --rm \
  -e AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  -e AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY \
  -e OBSIDIAN_EMAIL=$OBSIDIAN_EMAIL \
  -e OBSIDIAN_PASSWORD=$OBSIDIAN_PASSWORD \
  -e OBSIDIAN_TOTP_SECRET=$OBSIDIAN_TOTP_SECRET \
  -e S3_BUCKET=my-bucket \
  -e STATE_TABLE_NAME=obsync-state \
  -v "$PWD":/var/task \
  public.ecr.aws/lambda/go:1 \
  cmd/lambda/main.handler
```

## Performance Considerations

1. **Memory Allocation**: Start with 1GB and monitor actual usage
2. **Timeout Strategy**: Use 14.5 minutes to leave buffer for cleanup
3. **Batch Sizes**: Adjust based on file sizes and memory constraints
4. **Concurrency**: Limit to prevent memory exhaustion
5. **S3 Operations**: Use multipart uploads for large files

## Monitoring

Add CloudWatch custom metrics:

```go
// Log structured data for CloudWatch Insights
logger.WithFields(map[string]interface{}{
    "metric_type":    "sync_performance",
    "vault_id":       vaultID,
    "files_synced":   fileCount,
    "bytes_synced":   totalBytes,
    "duration_ms":    duration.Milliseconds(),
    "memory_used_mb": memoryUsed,
    "success":        success,
}).Info("Sync metrics")
```

## Summary

This implementation provides:

1. ✅ Full Lambda compatibility while maintaining CLI functionality
2. ✅ Memory-aware processing with automatic throttling
3. ✅ Batch processing for large vaults
4. ✅ Progress tracking across invocations
5. ✅ Error recovery for partial syncs
6. ✅ S3 and DynamoDB adapters implementing existing interfaces
7. ✅ Optimized for Lambda constraints (memory, timeout, /tmp)
8. ✅ Maintains backward compatibility with existing codebase

The infrastructure team can deploy this using their standard Lambda deployment process.