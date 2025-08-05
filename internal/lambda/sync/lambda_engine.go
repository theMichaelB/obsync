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
	engine         *sync.Engine
	transport      transport.Transport
	state          state.Store
	memManager     *MemoryManager
	batchProcessor *BatchProcessor
	startTime      time.Time
	timeoutBuffer  time.Duration
	logger         *events.Logger
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
	
	// Create default sync config
	syncConfig := &sync.SyncConfig{
		ChunkSize:     1024 * 1024, // 1MB
		MaxConcurrent: 5,
	}
	
	return &LambdaEngine{
		engine:         sync.NewEngine(transport, crypto, state, storage, syncConfig, logger),
		transport:      transport,
		state:          state,
		memManager:     memManager,
		batchProcessor: NewBatchProcessor(memManager),
		startTime:      time.Now(),
		timeoutBuffer:  30 * time.Second, // Stop 30s before Lambda timeout
		logger:         logger,
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
	return e.engine.Sync(ctx, vaultID, vaultHost, vaultKey, encryptionVersion, initial)
}

func (e *LambdaEngine) incrementalInitialSync(ctx context.Context, vaultID, vaultHost string,
	vaultKey []byte, encryptionVersion int) error {
	
	// Get vault state
	syncState, err := e.state.Load(vaultID)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	
	// Connect to WebSocket
	initMsg := models.InitMessage{
		Op:                "init",
		ID:                vaultID,
		Initial:           true,
		Version:           syncState.Version,
		EncryptionVersion: encryptionVersion,
	}
	
	msgChan, err := e.transport.StreamWS(ctx, vaultHost, initMsg)
	if err != nil {
		return fmt.Errorf("stream websocket: %w", err)
	}
	
	// Process messages in batches
	var fileBuffer []FileInfo
	
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
			
			fileBuffer = append(fileBuffer, FileInfo{
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
				syncState.LastSyncTime = time.Now()
				
				if err := e.state.Save(vaultID, syncState); err != nil {
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
	vaultKey []byte, files []FileInfo) error {
	
	return e.batchProcessor.ProcessFiles(ctx, files, 
		func(ctx context.Context, batch []FileInfo) error {
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

func (e *LambdaEngine) downloadAndStoreFile(ctx context.Context, vaultID string, 
	vaultKey []byte, file FileInfo) error {
	// This would implement the actual file download and storage logic
	// For now, just a placeholder that logs the operation
	e.logger.WithFields(map[string]interface{}{
		"vault_id": vaultID,
		"path":     file.Path,
		"size":     file.Size,
	}).Debug("Processing file")
	
	// TODO: Implement actual file download and storage
	return nil
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