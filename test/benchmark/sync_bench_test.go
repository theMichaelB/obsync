package benchmark

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/yourusername/obsync/internal/models"
	"github.com/yourusername/obsync/internal/services/sync"
	"github.com/yourusername/obsync/internal/state"
	"github.com/yourusername/obsync/test/testutil"
)

func BenchmarkSyncEngine(b *testing.B) {
	fileCounts := []int{10, 100, 1000}
	
	for _, count := range fileCounts {
		b.Run(fmt.Sprintf("%dFiles", count), func(b *testing.B) {
			// Setup
			mockTransport := testutil.NewMockTransport()
			mockCrypto := testutil.NewMockCryptoProvider()
			mockState := testutil.NewMockStateStore()
			mockStorage := testutil.NewMockBlobStore()
			
			engine := sync.NewEngine(
				mockTransport,
				mockCrypto,
				mockState,
				mockStorage,
				&sync.SyncConfig{
					MaxConcurrent: 10,
					ChunkSize:     1024 * 1024,
				},
				testutil.NewTestLogger(),
			)
			
			// Generate test messages
			messages := testutil.GenerateSyncMessages(count, 1024) // 1KB files
			mockTransport.SetWSMessages(messages)
			
			// Setup mock expectations
			mockState.On("Load", "bench-vault").Return(nil, state.ErrStateNotFound)
			mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
				func(hexPath string, key []byte) string {
					return testutil.DecryptMockPath(hexPath)
				}, nil,
			)
			mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
				func(data []byte, key []byte) []byte {
					return testutil.DecryptMockData(data)
				}, nil,
			)
			mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
			mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
			
			ctx := context.Background()
			vaultKey := make([]byte, 32)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				mockTransport.Reset()
				mockState.Reset()
				mockStorage.Reset()
				
				err := engine.Sync(ctx, "bench-vault", vaultKey, true)
				if err != nil {
					b.Fatal(err)
				}
			}
			
			b.ReportMetric(float64(count)/b.Elapsed().Seconds(), "files/sec")
		})
	}
}

func BenchmarkConcurrentFileProcessing(b *testing.B) {
	concurrencyLevels := []int{1, 5, 10, 20, 50}
	
	for _, level := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrent%d", level), func(b *testing.B) {
			mockTransport := testutil.NewMockTransport()
			mockCrypto := testutil.NewMockCryptoProvider()
			mockState := testutil.NewMockStateStore()
			mockStorage := testutil.NewMockBlobStore()
			
			engine := sync.NewEngine(
				mockTransport,
				mockCrypto,
				mockState,
				mockStorage,
				&sync.SyncConfig{
					MaxConcurrent: level,
					ChunkSize:     1024 * 1024,
				},
				testutil.NewTestLogger(),
			)
			
			// Generate 100 files
			messages := testutil.GenerateSyncMessages(100, 10240) // 10KB files
			mockTransport.SetWSMessages(messages)
			
			// Setup mocks
			mockState.On("Load", "bench-vault").Return(nil, state.ErrStateNotFound)
			mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
				func(hexPath string, key []byte) string {
					return testutil.DecryptMockPath(hexPath)
				}, nil,
			)
			mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
				func(data []byte, key []byte) []byte {
					return testutil.DecryptMockData(data)
				}, nil,
			)
			mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
			mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
			
			ctx := context.Background()
			vaultKey := make([]byte, 32)
			
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				mockTransport.Reset()
				
				err := engine.Sync(ctx, "bench-vault", vaultKey, true)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSyncEngineFileSizes(b *testing.B) {
	fileSizes := []int{
		1024,     // 1KB
		10240,    // 10KB
		102400,   // 100KB
		1048576,  // 1MB
	}
	
	for _, size := range fileSizes {
		b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
			mockTransport := testutil.NewMockTransport()
			mockCrypto := testutil.NewMockCryptoProvider()
			mockState := testutil.NewMockStateStore()
			mockStorage := testutil.NewMockBlobStore()
			
			engine := sync.NewEngine(
				mockTransport,
				mockCrypto,
				mockState,
				mockStorage,
				&sync.SyncConfig{
					MaxConcurrent: 5,
					ChunkSize:     1024 * 1024,
				},
				testutil.NewTestLogger(),
			)
			
			// Generate messages with specific file size
			messages := testutil.GenerateSyncMessages(10, size)
			mockTransport.SetWSMessages(messages)
			
			// Setup mocks
			mockState.On("Load", "bench-vault").Return(nil, state.ErrStateNotFound)
			mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
				func(hexPath string, key []byte) string {
					return testutil.DecryptMockPath(hexPath)
				}, nil,
			)
			mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
				func(data []byte, key []byte) []byte {
					return testutil.DecryptMockData(data)
				}, nil,
			)
			mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
			mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
			
			ctx := context.Background()
			vaultKey := make([]byte, 32)
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(10 * size)) // 10 files of specified size
			
			for i := 0; i < b.N; i++ {
				mockTransport.Reset()
				
				err := engine.Sync(ctx, "bench-vault", vaultKey, true)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkIncrementalSync(b *testing.B) {
	mockTransport := testutil.NewMockTransport()
	mockCrypto := testutil.NewMockCryptoProvider()
	mockState := testutil.NewMockStateStore()
	mockStorage := testutil.NewMockBlobStore()
	
	engine := sync.NewEngine(
		mockTransport,
		mockCrypto,
		mockState,
		mockStorage,
		&sync.SyncConfig{
			MaxConcurrent: 5,
			ChunkSize:     1024 * 1024,
		},
		testutil.NewTestLogger(),
	)
	
	// Setup existing state with 500 files
	existingState := testutil.SampleSyncState()
	for i := 0; i < 500; i++ {
		existingState.UpdateFile(fmt.Sprintf("file%d.md", i), fmt.Sprintf("hash%d", i))
	}
	
	// Incremental sync with only 10 new files
	messages := testutil.GenerateSyncMessages(10, 1024)
	mockTransport.SetWSMessages(messages)
	
	// Setup mocks
	mockState.On("Load", "bench-vault").Return(existingState, nil)
	mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
		func(hexPath string, key []byte) string {
			return testutil.DecryptMockPath(hexPath)
		}, nil,
	)
	mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
		func(data []byte, key []byte) []byte {
			return testutil.DecryptMockData(data)
		}, nil,
	)
	mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
	mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
	
	ctx := context.Background()
	vaultKey := make([]byte, 32)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		mockTransport.Reset()
		
		err := engine.Sync(ctx, "bench-vault", vaultKey, false) // Incremental
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSyncProgress(b *testing.B) {
	mockTransport := testutil.NewMockTransport()
	mockCrypto := testutil.NewMockCryptoProvider()
	mockState := testutil.NewMockStateStore()
	mockStorage := testutil.NewMockBlobStore()
	
	engine := sync.NewEngine(
		mockTransport,
		mockCrypto,
		mockState,
		mockStorage,
		&sync.SyncConfig{
			MaxConcurrent: 5,
			ChunkSize:     1024 * 1024,
		},
		testutil.NewTestLogger(),
	)
	
	messages := testutil.GenerateSyncMessages(100, 1024)
	mockTransport.SetWSMessages(messages)
	
	// Setup mocks
	mockState.On("Load", "bench-vault").Return(nil, state.ErrStateNotFound)
	mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
		func(hexPath string, key []byte) string {
			return testutil.DecryptMockPath(hexPath)
		}, nil,
	)
	mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
		func(data []byte, key []byte) []byte {
			return testutil.DecryptMockData(data)
		}, nil,
	)
	mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
	mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
	
	ctx := context.Background()
	vaultKey := make([]byte, 32)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		mockTransport.Reset()
		
		// Monitor progress events
		go func() {
			for event := range engine.Events() {
				// Consume events to prevent blocking
				_ = event
			}
		}()
		
		err := engine.Sync(ctx, "bench-vault", vaultKey, true)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSyncCancel(b *testing.B) {
	mockTransport := testutil.NewMockTransport()
	mockCrypto := testutil.NewMockCryptoProvider()
	mockState := testutil.NewMockStateStore()
	mockStorage := testutil.NewMockBlobStore()
	
	engine := sync.NewEngine(
		mockTransport,
		mockCrypto,
		mockState,
		mockStorage,
		&sync.SyncConfig{
			MaxConcurrent: 5,
			ChunkSize:     1024 * 1024,
		},
		testutil.NewTestLogger(),
	)
	
	// Large sync that we'll cancel
	messages := testutil.GenerateSyncMessages(1000, 1024)
	mockTransport.SetWSMessages(messages)
	mockTransport.SetDelay(1 * time.Millisecond) // Slow down for cancellation
	
	// Setup mocks
	mockState.On("Load", "bench-vault").Return(nil, state.ErrStateNotFound)
	mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
		func(hexPath string, key []byte) string {
			return testutil.DecryptMockPath(hexPath)
		}, nil,
	)
	mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
		func(data []byte, key []byte) []byte {
			return testutil.DecryptMockData(data)
		}, nil,
	)
	mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
	mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
	
	vaultKey := make([]byte, 32)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		mockTransport.Reset()
		
		ctx, cancel := context.WithCancel(context.Background())
		
		// Start sync
		syncDone := make(chan error, 1)
		go func() {
			syncDone <- engine.Sync(ctx, "bench-vault", vaultKey, true)
		}()
		
		// Cancel after short delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		
		// Wait for completion
		err := <-syncDone
		if err != nil && err != context.Canceled {
			b.Fatal(err)
		}
	}
}

func BenchmarkStateOperations(b *testing.B) {
	b.Run("LoadState", func(b *testing.B) {
		mockState := testutil.NewMockStateStore()
		state := testutil.SampleSyncState()
		
		// Add many files to state
		for i := 0; i < 1000; i++ {
			state.UpdateFile(fmt.Sprintf("file%d.md", i), fmt.Sprintf("hash%d", i))
		}
		
		mockState.On("Load", "vault-123").Return(state, nil)
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			_, err := mockState.Load("vault-123")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("SaveState", func(b *testing.B) {
		mockState := testutil.NewMockStateStore()
		state := testutil.SampleSyncState()
		
		// Add many files to state
		for i := 0; i < 1000; i++ {
			state.UpdateFile(fmt.Sprintf("file%d.md", i), fmt.Sprintf("hash%d", i))
		}
		
		mockState.On("Save", "vault-123", mock.Anything).Return(nil)
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			err := mockState.Save("vault-123", state)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemoryUsage(b *testing.B) {
	fileCounts := []int{100, 1000, 10000}
	
	for _, count := range fileCounts {
		b.Run(fmt.Sprintf("%dFiles", count), func(b *testing.B) {
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				// Simulate sync engine memory usage
				state := testutil.SampleSyncState()
				
				// Add files to state
				for j := 0; j < count; j++ {
					state.UpdateFile(
						fmt.Sprintf("file%d.md", j),
						fmt.Sprintf("hash%064d", j), // 64-char hash
					)
				}
				
				// Simulate processing
				for path, hash := range state.Files {
					_ = path + hash // Use the data
				}
			}
		})
	}
}

func BenchmarkRealisticSyncWorkload(b *testing.B) {
	// Simulate a realistic Obsidian vault sync
	mockTransport := testutil.NewMockTransport()
	mockCrypto := testutil.NewMockCryptoProvider()
	mockState := testutil.NewMockStateStore()
	mockStorage := testutil.NewMockBlobStore()
	
	engine := sync.NewEngine(
		mockTransport,
		mockCrypto,
		mockState,
		mockStorage,
		&sync.SyncConfig{
			MaxConcurrent: 8, // Realistic concurrency
			ChunkSize:     1024 * 1024,
		},
		testutil.NewTestLogger(),
	)
	
	// Create realistic file distribution
	var messages []models.WSMessage
	fileCount := 0
	
	// Add init message
	messages = append(messages, testutil.WSInitResponse(true, 500))
	
	// Small text files (80% of files)
	for i := 0; i < 400; i++ {
		fileCount++
		messages = append(messages, testutil.WSFileMessage(
			fileCount,
			fmt.Sprintf("notes/note%d.md", i),
			strings.Repeat("content", 50), // ~350 bytes
		))
	}
	
	// Medium files (15% of files)
	for i := 0; i < 75; i++ {
		fileCount++
		messages = append(messages, testutil.WSFileMessage(
			fileCount,
			fmt.Sprintf("daily/daily%d.md", i),
			strings.Repeat("daily content", 200), // ~2.6KB
		))
	}
	
	// Large files (5% of files)
	for i := 0; i < 25; i++ {
		fileCount++
		messages = append(messages, testutil.WSFileMessage(
			fileCount,
			fmt.Sprintf("attachments/file%d.txt", i),
			strings.Repeat("large file content", 1000), // ~18KB
		))
	}
	
	// Add done message
	messages = append(messages, testutil.WSDoneMessage(fileCount, fileCount))
	
	mockTransport.SetWSMessages(messages)
	
	// Setup mocks
	mockState.On("Load", "bench-vault").Return(nil, state.ErrStateNotFound)
	mockCrypto.On("DecryptPath", mock.Anything, mock.Anything).Return(
		func(hexPath string, key []byte) string {
			return testutil.DecryptMockPath(hexPath)
		}, nil,
	)
	mockCrypto.On("DecryptData", mock.Anything, mock.Anything).Return(
		func(data []byte, key []byte) []byte {
			return testutil.DecryptMockData(data)
		}, nil,
	)
	mockStorage.On("Write", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockState.On("Save", mock.Anything, mock.Anything).Return(nil)
	mockTransport.On("StreamWS", mock.Anything, mock.Anything).Return(nil)
	
	ctx := context.Background()
	vaultKey := make([]byte, 32)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		mockTransport.Reset()
		
		err := engine.Sync(ctx, "bench-vault", vaultKey, true)
		if err != nil {
			b.Fatal(err)
		}
	}
	
	b.ReportMetric(float64(fileCount)/b.Elapsed().Seconds(), "files/sec")
}