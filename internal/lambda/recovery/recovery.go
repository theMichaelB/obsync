package recovery

import (
	"context"
	"fmt"
	"time"
	
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/lambda/progress"
	"github.com/TheMichaelB/obsync/internal/models"
)

type RecoveryManager struct {
	logger          *events.Logger
	progressTracker *progress.ProgressTracker
}

func NewRecoveryManager(logger *events.Logger, progressTracker *progress.ProgressTracker) *RecoveryManager {
	return &RecoveryManager{
		logger:          logger,
		progressTracker: progressTracker,
	}
}

func (r *RecoveryManager) RecoverSync(ctx context.Context, vaultID string) (*models.SyncState, error) {
	// Check for previous progress
	syncProgress, err := r.progressTracker.GetProgress(ctx, vaultID)
	if err != nil {
		return nil, fmt.Errorf("get progress: %w", err)
	}
	
	if syncProgress == nil {
		r.logger.WithField("vault_id", vaultID).Info("No previous progress found")
		return models.NewSyncState(vaultID), nil
	}
	
	// Check if recovery is viable
	if !syncProgress.Resumable {
		r.logger.WithField("vault_id", vaultID).Warn("Previous sync not resumable")
		return models.NewSyncState(vaultID), nil
	}
	
	// Check age of progress
	age := time.Since(syncProgress.LastUpdate)
	if age > 1*time.Hour {
		r.logger.WithFields(map[string]interface{}{
			"vault_id": vaultID,
			"age":      age,
		}).Warn("Progress too old, starting fresh")
		return models.NewSyncState(vaultID), nil
	}
	
	r.logger.WithFields(map[string]interface{}{
		"vault_id":       vaultID,
		"files_complete": syncProgress.FilesComplete,
		"files_total":    syncProgress.FilesTotal,
	}).Info("Recovering previous sync")
	
	// Return state that can resume
	state := models.NewSyncState(vaultID)
	state.Version = syncProgress.LastVersion
	state.LastSyncTime = syncProgress.StartTime
	return state, nil
}

func (r *RecoveryManager) SaveSyncProgress(ctx context.Context, vaultID string, state *models.SyncState, 
	filesTotal, filesComplete int, bytesTotal, bytesComplete int64) error {
	
	syncProgress := progress.SyncProgress{
		VaultID:       vaultID,
		StartTime:     state.LastSyncTime,
		LastUpdate:    time.Now(),
		FilesTotal:    filesTotal,
		FilesComplete: filesComplete,
		BytesTotal:    bytesTotal,
		BytesComplete: bytesComplete,
		Status:        "running",
		Resumable:     true,
		LastVersion:   state.Version,
	}
	
	return r.progressTracker.SaveProgress(ctx, syncProgress)
}

func (r *RecoveryManager) MarkSyncComplete(ctx context.Context, vaultID string) error {
	syncProgress, err := r.progressTracker.GetProgress(ctx, vaultID)
	if err != nil {
		return err
	}
	
	if syncProgress != nil {
		syncProgress.Status = "complete"
		syncProgress.Resumable = false
		return r.progressTracker.SaveProgress(ctx, *syncProgress)
	}
	
	return nil
}

func (r *RecoveryManager) MarkSyncFailed(ctx context.Context, vaultID string, syncErr error) error {
	syncProgress, err := r.progressTracker.GetProgress(ctx, vaultID)
	if err != nil {
		return err
	}
	
	if syncProgress != nil {
		syncProgress.Status = "failed"
		syncProgress.Error = syncErr.Error()
		syncProgress.Resumable = true // Allow retry
		return r.progressTracker.SaveProgress(ctx, *syncProgress)
	}
	
	return nil
}