//go:build integration
// +build integration

package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TheMichaelB/obsync/internal/client"
	"github.com/TheMichaelB/obsync/internal/config"
	syncpkg "github.com/TheMichaelB/obsync/internal/services/sync"
	"github.com/TheMichaelB/obsync/test/testutil"
)

func TestFullSyncIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	// Setup test environment
	helpers := testutil.NewTestHelpers(t)
	defer helpers.Cleanup()
	
	fixture := testutil.LoadFixture("simple_vault")
	
	// Create test server
	server := testutil.NewTestServer()
	defer server.Close()
	
	// Configure server with test data
	server.AddVault(fixture.Vault)
	server.SetVaultKey(fixture.Vault.ID, fixture.VaultKey)
	server.LoadWSTrace(fixture.Vault.ID, fixture.WSTrace)
	
	// Create client
	cfg := &config.Config{
		API: config.APIConfig{
			BaseURL: server.URL,
			Timeout: 30 * time.Second,
		},
		Storage: config.StorageConfig{
			DataDir: filepath.Join(helpers.TempDir(), "data"),
		},
		Log: config.LogConfig{
			Level:  "debug",
			Format: "json",
		},
	}
	
	client, err := client.New(cfg, testutil.NewTestLogger())
	require.NoError(t, err)
	
	ctx, cancel := testutil.TestContext()
	defer cancel()
	
	// Login
	err = client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
	require.NoError(t, err)
	
	// List vaults
	vaults, err := client.Vaults.ListVaults(ctx)
	require.NoError(t, err)
	assert.Len(t, vaults, 1)
	assert.Equal(t, fixture.Vault.ID, vaults[0].ID)
	
	// Setup sync destination
	syncDest := filepath.Join(helpers.TempDir(), "vault")
	err = client.SetStorageBase(syncDest)
	require.NoError(t, err)
	
	client.Sync.SetCredentials(fixture.Email, fixture.Password)
	
	// Run sync
	var events []syncpkg.Event
	eventDone := make(chan struct{})
	
	go func() {
		defer close(eventDone)
		for event := range client.Sync.Events() {
			events = append(events, event)
			t.Logf("Sync event: %s", event.Type)
		}
	}()
	
	err = client.Sync.SyncVault(ctx, fixture.Vault.ID, syncpkg.SyncOptions{
		Initial: true,
	})
	require.NoError(t, err)
	
	// Wait for events to complete
	select {
	case <-eventDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for sync events")
	}
	
	// Verify files
	for _, expectedFile := range fixture.ExpectedFiles {
		path := filepath.Join(syncDest, expectedFile.Path)
		
		// Check file exists
		helpers.AssertFileExists(path)
		
		// Verify content for text files
		if !expectedFile.Binary {
			helpers.AssertFileContent(path, expectedFile.Content)
		}
	}
	
	// Verify events
	assert.NotEmpty(t, events)
	
	var startEvent, completeEvent *syncpkg.Event
	fileEvents := make(map[string]*syncpkg.Event)
	
	for i := range events {
		event := &events[i]
		switch event.Type {
		case syncpkg.EventStarted:
			startEvent = event
		case syncpkg.EventCompleted:
			completeEvent = event
		case syncpkg.EventFileComplete:
			if event.File != nil {
				fileEvents[event.File.Path] = event
			}
		}
	}
	
	assert.NotNil(t, startEvent, "Should have start event")
	assert.NotNil(t, completeEvent, "Should have complete event")
	assert.Equal(t, len(fixture.ExpectedFiles), len(fileEvents), "Should have file events for all files")
}

func TestIncrementalSyncIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	helpers := testutil.NewTestHelpers(t)
	defer helpers.Cleanup()
	
	server := testutil.NewTestServer()
	defer server.Close()
	
	// Setup incremental sync fixture
	fixture := testutil.LoadFixture("incremental_vault")
	server.AddVault(fixture.Vault)
	server.SetVaultKey(fixture.Vault.ID, fixture.VaultKey)
	
	cfg := testutil.TestConfigWithDir(helpers.TempDir())
	cfg.API.BaseURL = server.URL
	
	client, err := client.New(cfg, testutil.NewTestLogger())
	require.NoError(t, err)
	
	ctx, cancel := testutil.TestContext()
	defer cancel()
	
	syncDest := filepath.Join(helpers.TempDir(), "vault")
	
	// Login and setup
	err = client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
	require.NoError(t, err)
	
	err = client.SetStorageBase(syncDest)
	require.NoError(t, err)
	
	client.Sync.SetCredentials(fixture.Email, fixture.Password)
	
	// First sync - initial files
	if fixture.InitialTrace != "" {
		server.LoadWSTrace(fixture.Vault.ID, fixture.InitialTrace)
	}
	
	err = client.Sync.SyncVault(ctx, fixture.Vault.ID, syncpkg.SyncOptions{
		Initial: true,
	})
	require.NoError(t, err)
	
	// Verify initial files
	initialFiles := []string{"notes/first.md", "daily/2024-01-01.md"}
	for _, file := range initialFiles {
		helpers.AssertFileExists(filepath.Join(syncDest, file))
	}
	
	// Second sync - incremental changes
	if fixture.IncrementalTrace != "" {
		server.LoadWSTrace(fixture.Vault.ID, fixture.IncrementalTrace)
	}
	
	err = client.Sync.SyncVault(ctx, fixture.Vault.ID, syncpkg.SyncOptions{
		Initial: false,
	})
	require.NoError(t, err)
	
	// Verify changes
	helpers.AssertFileExists(filepath.Join(syncDest, "notes/new.md"))
	helpers.AssertFileNotExists(filepath.Join(syncDest, "daily/2024-01-01.md")) // Should be deleted
	
	// Verify modified file
	content, err := os.ReadFile(filepath.Join(syncDest, "notes/first.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "updated content")
}

func TestSyncErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	helpers := testutil.NewTestHelpers(t)
	defer helpers.Cleanup()
	
	server := testutil.NewTestServer()
	defer server.Close()
	
	fixture := testutil.LoadFixture("error_vault")
	server.AddVault(fixture.Vault)
	server.SetVaultKey(fixture.Vault.ID, fixture.VaultKey)
	server.LoadWSTrace(fixture.Vault.ID, fixture.WSTrace)
	
	// Set up custom login handler that fails
	server.SetLoginHandler(func(email, password string) (string, error) {
		if email == "fail@example.com" {
			return "", errors.New("authentication failed")
		}
		return "valid-token", nil
	})
	
	cfg := testutil.TestConfigWithDir(helpers.TempDir())
	cfg.API.BaseURL = server.URL
	
	client, err := client.New(cfg, testutil.NewTestLogger())
	require.NoError(t, err)
	
	ctx, cancel := testutil.TestContext()
	defer cancel()
	
	t.Run("AuthenticationError", func(t *testing.T) {
		err := client.Auth.Login(ctx, "fail@example.com", "password", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})
	
	t.Run("VaultNotFound", func(t *testing.T) {
		err := client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
		require.NoError(t, err)
		
		err = client.Sync.SyncVault(ctx, "non-existent-vault", syncpkg.SyncOptions{})
		assert.Error(t, err)
	})
	
	t.Run("InvalidDestination", func(t *testing.T) {
		err := client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
		require.NoError(t, err)
		
		// Try to set invalid storage base
		err = client.SetStorageBase("/invalid/path/that/does/not/exist")
		assert.Error(t, err)
	})
}

func TestSyncCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	helpers := testutil.NewTestHelpers(t)
	defer helpers.Cleanup()
	
	server := testutil.NewTestServer()
	defer server.Close()
	
	fixture := testutil.LoadFixture("large_vault")
	server.AddVault(fixture.Vault)
	server.SetVaultKey(fixture.Vault.ID, fixture.VaultKey)
	server.LoadWSTrace(fixture.Vault.ID, fixture.WSTrace)
	
	cfg := testutil.TestConfigWithDir(helpers.TempDir())
	cfg.API.BaseURL = server.URL
	
	client, err := client.New(cfg, testutil.NewTestLogger())
	require.NoError(t, err)
	
	// Login
	ctx := context.Background()
	err = client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
	require.NoError(t, err)
	
	syncDest := filepath.Join(helpers.TempDir(), "vault")
	err = client.SetStorageBase(syncDest)
	require.NoError(t, err)
	
	client.Sync.SetCredentials(fixture.Email, fixture.Password)
	
	// Start sync with cancellable context
	syncCtx, cancel := context.WithCancel(ctx)
	
	syncDone := make(chan error, 1)
	go func() {
		syncDone <- client.Sync.SyncVault(syncCtx, fixture.Vault.ID, syncpkg.SyncOptions{
			Initial: true,
		})
	}()
	
	// Cancel after short delay
	time.Sleep(100 * time.Millisecond)
	cancel()
	
	// Should complete with cancellation error
	select {
	case err := <-syncDone:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for sync cancellation")
	}
}

func TestSyncResume(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	helpers := testutil.NewTestHelpers(t)
	defer helpers.Cleanup()
	
	server := testutil.NewTestServer()
	defer server.Close()
	
	fixture := testutil.LoadFixture("resume_vault")
	server.AddVault(fixture.Vault)
	server.SetVaultKey(fixture.Vault.ID, fixture.VaultKey)
	server.LoadWSTrace(fixture.Vault.ID, fixture.WSTrace)
	
	cfg := testutil.TestConfigWithDir(helpers.TempDir())
	cfg.API.BaseURL = server.URL
	
	client, err := client.New(cfg, testutil.NewTestLogger())
	require.NoError(t, err)
	
	ctx, cancel := testutil.TestContext()
	defer cancel()
	
	// Login
	err = client.Auth.Login(ctx, fixture.Email, fixture.Password, "")
	require.NoError(t, err)
	
	syncDest := filepath.Join(helpers.TempDir(), "vault")
	err = client.SetStorageBase(syncDest)
	require.NoError(t, err)
	
	client.Sync.SetCredentials(fixture.Email, fixture.Password)
	
	// First sync - partial
	err = client.Sync.SyncVault(ctx, fixture.Vault.ID, syncpkg.SyncOptions{
		Initial: true,
	})
	require.NoError(t, err)
	
	// Verify some files exist
	partialFiles := []string{"notes/file1.md", "notes/file2.md"}
	for _, file := range partialFiles {
		helpers.AssertFileExists(filepath.Join(syncDest, file))
	}
	
	// Second sync - resume (should be incremental)
	err = client.Sync.SyncVault(ctx, fixture.Vault.ID, syncpkg.SyncOptions{
		Initial: false, // Resume from previous state
	})
	require.NoError(t, err)
	
	// Verify all files now exist
	for _, expectedFile := range fixture.ExpectedFiles {
		helpers.AssertFileExists(filepath.Join(syncDest, expectedFile.Path))
	}
}

func TestConcurrentSyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	
	helpers := testutil.NewTestHelpers(t)
	defer helpers.Cleanup()
	
	server := testutil.NewTestServer()
	defer server.Close()
	
	// Create multiple vaults
	vault1 := testutil.LoadFixture("vault1")
	vault2 := testutil.LoadFixture("vault2")
	
	server.AddVault(vault1.Vault)
	server.AddVault(vault2.Vault)
	server.SetVaultKey(vault1.Vault.ID, vault1.VaultKey)
	server.SetVaultKey(vault2.Vault.ID, vault2.VaultKey)
	server.LoadWSTrace(vault1.Vault.ID, vault1.WSTrace)
	server.LoadWSTrace(vault2.Vault.ID, vault2.WSTrace)
	
	cfg := testutil.TestConfigWithDir(helpers.TempDir())
	cfg.API.BaseURL = server.URL
	
	client, err := client.New(cfg, testutil.NewTestLogger())
	require.NoError(t, err)
	
	ctx, cancel := testutil.TestContext()
	defer cancel()
	
	// Login
	err = client.Auth.Login(ctx, vault1.Email, vault1.Password, "")
	require.NoError(t, err)
	
	client.Sync.SetCredentials(vault1.Email, vault1.Password)
	
	// Sync both vaults concurrently
	syncDest1 := filepath.Join(helpers.TempDir(), "vault1")
	syncDest2 := filepath.Join(helpers.TempDir(), "vault2")
	
	var wg sync.WaitGroup
	var errs []error
	var mu sync.Mutex
	
	wg.Add(2)
	
	// Sync vault 1
	go func() {
		defer wg.Done()
		
		err := client.SetStorageBase(syncDest1)
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		
		err = client.Sync.SyncVault(ctx, vault1.Vault.ID, syncpkg.SyncOptions{Initial: true})
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}()
	
	// Sync vault 2
	go func() {
		defer wg.Done()
		
		err := client.SetStorageBase(syncDest2)
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		
		err = client.Sync.SyncVault(ctx, vault2.Vault.ID, syncpkg.SyncOptions{Initial: true})
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}()
	
	wg.Wait()
	
	// Check for errors
	require.Empty(t, errs, "Concurrent syncs should succeed")
	
	// Verify files for both vaults
	for _, expectedFile := range vault1.ExpectedFiles {
		helpers.AssertFileExists(filepath.Join(syncDest1, expectedFile.Path))
	}
	
	for _, expectedFile := range vault2.ExpectedFiles {
		helpers.AssertFileExists(filepath.Join(syncDest2, expectedFile.Path))
	}
}