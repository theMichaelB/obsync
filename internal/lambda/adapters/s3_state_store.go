package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/state"
)

type S3StateStore struct {
	client       *s3.Client
	bucket       string
	statePrefix  string
	logger       *events.Logger
	localCache   map[string]*CachedState
	cacheTimeout time.Duration
}

type CachedState struct {
	State     *models.SyncState
	VersionID string
	CachedAt  time.Time
}

type StateMetadata struct {
	VaultID   string    `json:"vault_id"`
	VersionID string    `json:"version_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewS3StateStore(bucket, statePrefix string, logger *events.Logger) (*S3StateStore, error) {
	if bucket == "" {
		bucket = os.Getenv("S3_BUCKET")
		if bucket == "" {
			return nil, fmt.Errorf("S3_BUCKET environment variable required")
		}
	}

	if statePrefix == "" {
		statePrefix = os.Getenv("S3_STATE_PREFIX")
		if statePrefix == "" {
			statePrefix = "state/"
		}
	}

	// Ensure prefix ends with /
	if !strings.HasSuffix(statePrefix, "/") {
		statePrefix += "/"
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &S3StateStore{
		client:       s3.NewFromConfig(cfg),
		bucket:       bucket,
		statePrefix:  statePrefix,
		logger:       logger.WithField("component", "s3_state_store"),
		localCache:   make(map[string]*CachedState),
		cacheTimeout: 5 * time.Minute,
	}, nil
}

// Implement state.Store interface

func (s *S3StateStore) Load(vaultID string) (*models.SyncState, error) {
	// Check local cache first
	if cached, exists := s.localCache[vaultID]; exists {
		if time.Since(cached.CachedAt) < s.cacheTimeout {
			s.logger.WithField("vault_id", vaultID).Debug("Using cached state")
			return cached.State, nil
		}
		// Cache expired, remove it
		delete(s.localCache, vaultID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stateKey := s.statePrefix + vaultID + ".json"

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(stateKey),
	})

	if err != nil {
		// Check if object doesn't exist
		if strings.Contains(err.Error(), "NoSuchKey") {
			s.logger.WithField("vault_id", vaultID).Debug("No state found, creating new")
			newState := models.NewSyncState(vaultID)
			return newState, nil
		}
		return nil, fmt.Errorf("s3 get state: %w", err)
	}
	defer result.Body.Close()

	var syncState models.SyncState
	if err := json.NewDecoder(result.Body).Decode(&syncState); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	// Cache the state with version ID
	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}

	s.localCache[vaultID] = &CachedState{
		State:     &syncState,
		VersionID: versionID,
		CachedAt:  time.Now(),
	}

	s.logger.WithFields(map[string]interface{}{
		"vault_id":   vaultID,
		"version_id": versionID,
	}).Debug("Loaded state from S3")

	return &syncState, nil
}

func (s *S3StateStore) Save(vaultID string, syncState *models.SyncState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stateJSON, err := json.MarshalIndent(syncState, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	stateKey := s.statePrefix + vaultID + ".json"

	// Get current version ID if we have it cached
	var ifMatch *string
	if cached, exists := s.localCache[vaultID]; exists && cached.VersionID != "" {
		ifMatch = aws.String(cached.VersionID)
	}

	putInput := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(stateKey),
		Body:        strings.NewReader(string(stateJSON)),
		ContentType: aws.String("application/json"),
		Metadata: map[string]string{
			"vault-id":   vaultID,
			"updated-at": time.Now().UTC().Format(time.RFC3339),
			"version":    fmt.Sprintf("%d", syncState.Version),
		},
	}

	// Use conditional write if we have a version ID
	if ifMatch != nil {
		putInput.IfMatch = ifMatch
	}

	result, err := s.client.PutObject(ctx, putInput)
	if err != nil {
		// Handle conditional write failure
		if strings.Contains(err.Error(), "PreconditionFailed") {
			s.logger.WithField("vault_id", vaultID).Warn("State was modified by another process, retrying")
			// Clear cache and retry once
			delete(s.localCache, vaultID)
			return s.Save(vaultID, syncState)
		}
		return fmt.Errorf("s3 put state: %w", err)
	}

	// Update cache with new version ID
	newVersionID := ""
	if result.VersionId != nil {
		newVersionID = *result.VersionId
	}

	s.localCache[vaultID] = &CachedState{
		State:     syncState,
		VersionID: newVersionID,
		CachedAt:  time.Now(),
	}

	s.logger.WithFields(map[string]interface{}{
		"vault_id":       vaultID,
		"new_version_id": newVersionID,
		"sync_version":   syncState.Version,
	}).Debug("Saved state to S3")

	return nil
}

func (s *S3StateStore) Reset(vaultID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stateKey := s.statePrefix + vaultID + ".json"

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(stateKey),
	})

	if err != nil {
		return fmt.Errorf("s3 delete state: %w", err)
	}

	// Clear from cache
	delete(s.localCache, vaultID)

	s.logger.WithField("vault_id", vaultID).Info("Reset state in S3")
	return nil
}

func (s *S3StateStore) List() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var vaultIDs []string

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.statePrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}

			// Extract vault ID from key (remove prefix and .json suffix)
			key := *obj.Key
			vaultID := strings.TrimPrefix(key, s.statePrefix)
			vaultID = strings.TrimSuffix(vaultID, ".json")

			if vaultID != "" && vaultID != key { // Make sure we actually removed something
				vaultIDs = append(vaultIDs, vaultID)
			}
		}
	}

	return vaultIDs, nil
}

func (s *S3StateStore) Lock(vaultID string) (state.UnlockFunc, error) {
	// For Lambda, we rely on S3 conditional writes instead of explicit locks
	// since Lambda functions are stateless and short-lived
	s.logger.WithField("vault_id", vaultID).Debug("Lock requested (using S3 conditional writes)")

	return func() {
		s.logger.WithField("vault_id", vaultID).Debug("Unlock called")
	}, nil
}

func (s *S3StateStore) Migrate(target state.Store) error {
	vaultIDs, err := s.List()
	if err != nil {
		return fmt.Errorf("list vaults: %w", err)
	}

	for _, vaultID := range vaultIDs {
		state, err := s.Load(vaultID)
		if err != nil {
			return fmt.Errorf("load vault %s: %w", vaultID, err)
		}

		if err := target.Save(vaultID, state); err != nil {
			return fmt.Errorf("save vault %s: %w", vaultID, err)
		}
	}

	return nil
}

func (s *S3StateStore) Close() error {
	// Clear cache
	s.localCache = make(map[string]*CachedState)
	return nil
}

// DownloadStatesOnStartup downloads all states at startup for faster access
func (s *S3StateStore) DownloadStatesOnStartup(ctx context.Context) error {
	s.logger.Info("Downloading states on startup for local caching")

	vaultIDs, err := s.List()
	if err != nil {
		return fmt.Errorf("list vault states: %w", err)
	}

	s.logger.WithField("vault_count", len(vaultIDs)).Info("Found vault states to cache")

	// Download states concurrently but with limited concurrency
	sem := make(chan struct{}, 5) // Max 5 concurrent downloads
	errChan := make(chan error, len(vaultIDs))

	for _, vaultID := range vaultIDs {
		sem <- struct{}{}
		go func(id string) {
			defer func() { <-sem }()

			_, err := s.Load(id) // This will cache the state
			if err != nil {
				errChan <- fmt.Errorf("load state for %s: %w", id, err)
			} else {
				errChan <- nil
			}
		}(vaultID)
	}

	// Wait for all downloads and collect errors
	var errors []error
	for i := 0; i < len(vaultIDs); i++ {
		if err := <-errChan; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		s.logger.WithField("error_count", len(errors)).Warn("Some states failed to download")
		return fmt.Errorf("failed to download %d states: %v", len(errors), errors[0])
	}

	s.logger.WithField("cached_states", len(s.localCache)).Info("Successfully cached all states")
	return nil
}