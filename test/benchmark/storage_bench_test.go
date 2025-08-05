package benchmark

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/storage"
	"github.com/TheMichaelB/obsync/test/testutil"
)

func BenchmarkBlobStoreWrite(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	sizes := []int{
		1024,     // 1KB
		10240,    // 10KB
		102400,   // 100KB
		1048576,  // 1MB
		10485760, // 10MB
	}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
			data := make([]byte, size)
			rand.Read(data)
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				path := fmt.Sprintf("bench/file_%d.dat", i)
				err := store.Write(path, data, 0644)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBlobStoreRead(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	sizes := []int{
		1024,     // 1KB
		10240,    // 10KB
		102400,   // 100KB
		1048576,  // 1MB
		10485760, // 10MB
	}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
			data := make([]byte, size)
			rand.Read(data)
			
			// Pre-create file
			path := "bench/read_test.dat"
			err := store.Write(path, data, 0644)
			if err != nil {
				b.Fatal(err)
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				_, err := store.Read(path)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBlobStoreOperations(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	data := make([]byte, 1024) // 1KB
	rand.Read(data)
	
	b.Run("Exists", func(b *testing.B) {
		// Pre-create some files
		for i := 0; i < 100; i++ {
			path := fmt.Sprintf("bench/exists_%d.dat", i)
			store.Write(path, data, 0644)
		}
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			path := fmt.Sprintf("bench/exists_%d.dat", i%100)
			_, err := store.Exists(path)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("Size", func(b *testing.B) {
		// Pre-create file
		path := "bench/size_test.dat"
		store.Write(path, data, 0644)
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			_, err := store.Size(path)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("Delete", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			path := fmt.Sprintf("bench/delete_%d.dat", i)
			
			// Create file
			store.Write(path, data, 0644)
			
			// Delete it (this is what we're benchmarking)
			err := store.Delete(path)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkAtomicWrites(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	data := make([]byte, 10240) // 10KB
	rand.Read(data)
	
	modes := []os.FileMode{
		0644,
		0755,
		0600,
	}
	
	for _, mode := range modes {
		b.Run(fmt.Sprintf("Mode_%s", mode), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			
			for i := 0; i < b.N; i++ {
				path := fmt.Sprintf("bench/atomic_%d.dat", i)
				err := store.Write(path, data, mode)
				if err != nil && mode != os.FileModeUpdate { // Update might fail if file doesn't exist
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkConcurrentAccess(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	data := make([]byte, 1024)
	rand.Read(data)
	
	// Pre-create files for reading
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("bench/concurrent_%d.dat", i)
		store.Write(path, data, 0644)
	}
	
	b.Run("ConcurrentReads", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				path := fmt.Sprintf("bench/concurrent_%d.dat", i%100)
				_, err := store.Read(path)
				if err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	})
	
	b.Run("ConcurrentWrites", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				path := fmt.Sprintf("bench/concurrent_write_%d.dat", i)
				err := store.Write(path, data, 0644)
				if err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	})
	
	b.Run("MixedOperations", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				switch i % 4 {
				case 0: // Read
					path := fmt.Sprintf("bench/concurrent_%d.dat", i%100)
					store.Read(path)
				case 1: // Write
					path := fmt.Sprintf("bench/mixed_write_%d.dat", i)
					store.Write(path, data, 0644)
				case 2: // Exists
					path := fmt.Sprintf("bench/concurrent_%d.dat", i%100)
					store.Exists(path)
				case 3: // Size
					path := fmt.Sprintf("bench/concurrent_%d.dat", i%100)
					store.Size(path)
				}
				i++
			}
		})
	})
}

func BenchmarkDirectoryOperations(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	data := make([]byte, 1024)
	rand.Read(data)
	
	b.Run("DeepPaths", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		
		for i := 0; i < b.N; i++ {
			// Create deeply nested path
			path := fmt.Sprintf("bench/level1/level2/level3/level4/level5/file_%d.dat", i)
			err := store.Write(path, data, 0644)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("ManyFiles", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		
		for i := 0; i < b.N; i++ {
			// Create many files in same directory
			path := fmt.Sprintf("bench/many/file_%d.dat", i)
			err := store.Write(path, data, 0644)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkPathSanitization(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	data := make([]byte, 1024)
	rand.Read(data)
	
	// Test paths that need sanitization
	problematicPaths := []string{
		"../outside/path.dat",
		"path/with spaces/file.dat",
		"path/with-unicode-üñíçødé/file.dat",
		"path/with.dots.and..double/file.dat",
		"very/long/path/that/goes/many/levels/deep/and/continues/further/down/file.dat",
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	
	for i := 0; i < b.N; i++ {
		path := problematicPaths[i%len(problematicPaths)]
		path = fmt.Sprintf("%s_%d", path, i) // Make unique
		
		err := store.Write(path, data, 0644)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBinaryDetection(b *testing.B) {
	// Test binary detection performance
	textData := []byte(strings.Repeat("This is text content. ", 100))
	binaryData := make([]byte, 2048)
	rand.Read(binaryData)
	
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG header
	}
	pngData = append(pngData, make([]byte, 2040)...)
	rand.Read(pngData[8:])
	
	tests := []struct {
		name string
		data []byte
	}{
		{"Text", textData},
		{"Binary", binaryData},
		{"PNG", pngData},
	}
	
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(test.data)))
			
			for i := 0; i < b.N; i++ {
				models.IsBinary(test.data)
			}
		})
	}
}

func BenchmarkBufferOperations(b *testing.B) {
	sizes := []int{1024, 10240, 102400, 1048576}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("BufferReuse_%dKB", size/1024), func(b *testing.B) {
			data := make([]byte, size)
			rand.Read(data)
			
			// Pre-allocate buffer pool
			bufferPool := make([][]byte, 10)
			for i := range bufferPool {
				bufferPool[i] = make([]byte, 0, size+1024) // Extra capacity
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				// Get buffer from pool
				buf := bufferPool[i%len(bufferPool)]
				buf = buf[:0] // Reset length
				
				// Simulate processing
				buf = append(buf, data...)
				
				// Simulate some processing
				_ = bytes.Contains(buf, []byte("test"))
			}
		})
		
		b.Run(fmt.Sprintf("NewBuffer_%dKB", size/1024), func(b *testing.B) {
			data := make([]byte, size)
			rand.Read(data)
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				// Create new buffer each time
				buf := make([]byte, 0, size)
				buf = append(buf, data...)
				
				// Simulate some processing
				_ = bytes.Contains(buf, []byte("test"))
			}
		})
	}
}

func BenchmarkRealisticStorageWorkload(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	// Simulate realistic Obsidian vault files
	files := map[string][]byte{
		"notes/welcome.md":         []byte(testutil.SampleFiles["notes/welcome.md"]),
		"daily/2024-01-15.md":      []byte(testutil.SampleFiles["daily/2024-01-15.md"]),
		"concepts/testing.md":      []byte(testutil.SampleFiles["concepts/testing.md"]),
		"attachments/readme.txt":   []byte(testutil.SampleFiles["attachments/readme.txt"]),
		"attachments/example.png":  testutil.SampleBinaryFiles["attachments/example.png"],
		"attachments/document.pdf": testutil.SampleBinaryFiles["attachments/document.pdf"],
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	totalSize := 0
	for _, data := range files {
		totalSize += len(data)
	}
	b.SetBytes(int64(totalSize))
	
	for i := 0; i < b.N; i++ {
		// Write all files
		for path, data := range files {
			uniquePath := fmt.Sprintf("%s_%d", path, i)
			err := store.Write(uniquePath, data, 0644)
			if err != nil {
				b.Fatal(err)
			}
		}
		
		// Read some files back
		for path := range files {
			uniquePath := fmt.Sprintf("%s_%d", path, i)
			_, err := store.Read(uniquePath)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkStorageWithEncryption(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	// Simulate encrypted data
	sizes := []int{1024, 10240, 102400}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Encrypted_%dKB", size/1024), func(b *testing.B) {
			// Generate random data (simulating encrypted content)
			data := make([]byte, size)
			rand.Read(data)
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				path := fmt.Sprintf("encrypted/file_%d.enc", i)
				
				// Write encrypted data
				err := store.Write(path, data, 0644)
				if err != nil {
					b.Fatal(err)
				}
				
				// Read it back
				readData, err := store.Read(path)
				if err != nil {
					b.Fatal(err)
				}
				
				// Verify size (simulating decryption check)
				if len(readData) != size {
					b.Fatalf("Size mismatch: expected %d, got %d", size, len(readData))
				}
			}
		})
	}
}

func BenchmarkFileSystemLimits(b *testing.B) {
	tempDir := b.TempDir()
	logger := testutil.NewTestLogger()
	store, err := storage.NewLocalStore(tempDir, logger)
	if err != nil {
		b.Fatal(err)
	}
	
	data := make([]byte, 1024)
	rand.Read(data)
	
	b.Run("ManySmallFiles", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		
		for i := 0; i < b.N; i++ {
			path := fmt.Sprintf("small/file_%06d.dat", i)
			err := store.Write(path, data, 0644)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("LongPaths", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		
		for i := 0; i < b.N; i++ {
			// Create very long path (close to filesystem limits)
			longPath := fmt.Sprintf("long/%s/file_%d.dat",
				strings.Repeat("verylongdirectoryname/", 10), i)
			
			err := store.Write(longPath, data, 0644)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}