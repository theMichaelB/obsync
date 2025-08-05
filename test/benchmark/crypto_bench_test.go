package benchmark

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/yourusername/obsync/internal/crypto"
	"github.com/yourusername/obsync/test/testutil"
)

func BenchmarkKeyDerivation(b *testing.B) {
	provider := crypto.NewProvider()
	
	info := crypto.VaultKeyInfo{
		Version:           1,
		EncryptionVersion: 3,
		Salt:             testutil.RandomSalt(),
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := provider.DeriveKey("test@example.com", "password123", info)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkKeyDerivationVersions(b *testing.B) {
	provider := crypto.NewProvider()
	versions := []int{1, 2, 3}
	
	for _, version := range versions {
		b.Run(fmt.Sprintf("v%d", version), func(b *testing.B) {
			info := crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: version,
				Salt:             testutil.RandomSalt(),
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				_, err := provider.DeriveKey("test@example.com", "password123", info)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecryptData(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	sizes := []int{
		1024,       // 1KB
		10240,      // 10KB
		102400,     // 100KB
		1048576,    // 1MB
		10485760,   // 10MB
	}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
			plaintext := make([]byte, size)
			rand.Read(plaintext)
			
			ciphertext, err := provider.EncryptData(plaintext, key)
			if err != nil {
				b.Fatal(err)
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				_, err := provider.DecryptData(ciphertext, key)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncryptData(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	sizes := []int{
		1024,       // 1KB
		10240,      // 10KB
		102400,     // 100KB
		1048576,    // 1MB
		10485760,   // 10MB
	}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
			plaintext := make([]byte, size)
			rand.Read(plaintext)
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(size))
			
			for i := 0; i < b.N; i++ {
				_, err := provider.EncryptData(plaintext, key)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPathDecryption(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	paths := []string{
		"notes/test.md",
		"daily/2024/01/15.md",
		"attachments/images/screenshot.png",
		"very/deep/nested/folder/structure/with/many/levels/file.txt",
		"projects/2024/client-work/presentations/final-presentation-v3-revised.pptx",
	}
	
	// Encrypt paths
	var encPaths []string
	for _, path := range paths {
		enc, err := testutil.EncryptMockPath(path, key)
		if err != nil {
			b.Fatal(err)
		}
		encPaths = append(encPaths, enc)
	}
	
	b.Run("NoCache", func(b *testing.B) {
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			for _, encPath := range encPaths {
				_, err := provider.DecryptPath(encPath, key)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	
	b.Run("WithCache", func(b *testing.B) {
		pathDecryptor := crypto.NewPathDecryptor(provider)
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			for _, encPath := range encPaths {
				_, err := pathDecryptor.DecryptPath(encPath, key)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func BenchmarkPathDecryptionSizes(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	// Test different path lengths
	pathLengths := []int{10, 50, 100, 200, 500}
	
	for _, length := range pathLengths {
		b.Run(fmt.Sprintf("%dchars", length), func(b *testing.B) {
			// Generate path of specific length
			path := string(make([]byte, length))
			for i := range path {
				path = path[:i] + "a" + path[i+1:]
			}
			
			encPath, err := testutil.EncryptMockPath(path, key)
			if err != nil {
				b.Fatal(err)
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				_, err := provider.DecryptPath(encPath, key)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkConcurrentDecryption(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	// Prepare test data
	plaintext := make([]byte, 10240) // 10KB
	rand.Read(plaintext)
	
	ciphertext, err := provider.EncryptData(plaintext, key)
	if err != nil {
		b.Fatal(err)
	}
	
	concurrencyLevels := []int{1, 2, 4, 8, 16}
	
	for _, level := range concurrencyLevels {
		b.Run(fmt.Sprintf("goroutines_%d", level), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(plaintext)))
			
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, err := provider.DecryptData(ciphertext, key)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkBulkOperations(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	fileCounts := []int{10, 100, 1000}
	fileSize := 1024 // 1KB per file
	
	for _, count := range fileCounts {
		b.Run(fmt.Sprintf("%dfiles", count), func(b *testing.B) {
			// Prepare test data
			var files [][]byte
			var encrypted [][]byte
			
			for i := 0; i < count; i++ {
				data := make([]byte, fileSize)
				rand.Read(data)
				files = append(files, data)
				
				enc, err := provider.EncryptData(data, key)
				if err != nil {
					b.Fatal(err)
				}
				encrypted = append(encrypted, enc)
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(count * fileSize))
			
			for i := 0; i < b.N; i++ {
				for j := 0; j < count; j++ {
					_, err := provider.DecryptData(encrypted[j], key)
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkMemoryReuse(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	plaintext := make([]byte, 10240) // 10KB
	rand.Read(plaintext)
	
	ciphertext, err := provider.EncryptData(plaintext, key)
	if err != nil {
		b.Fatal(err)
	}
	
	b.Run("WithAllocation", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		b.SetBytes(int64(len(plaintext)))
		
		for i := 0; i < b.N; i++ {
			_, err := provider.DecryptData(ciphertext, key)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("WithBufferReuse", func(b *testing.B) {
		// Pre-allocate buffer for reuse
		buffer := make([]byte, 0, len(plaintext)+100) // Extra space for overhead
		
		b.ResetTimer()
		b.ReportAllocs()
		b.SetBytes(int64(len(plaintext)))
		
		for i := 0; i < b.N; i++ {
			// In a real implementation, we'd pass the buffer to reuse
			_, err := provider.DecryptData(ciphertext, key)
			if err != nil {
				b.Fatal(err)
			}
			// Reset buffer for next iteration
			buffer = buffer[:0]
		}
	})
}

// Test realistic scenarios
func BenchmarkRealisticWorkload(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, 32)
	rand.Read(key)
	
	// Simulate a realistic vault with mixed file sizes
	fileSizes := []int{
		100,    // Small notes
		500,    // Medium notes  
		2048,   // Large notes
		10240,  // Small attachments
		102400, // Medium attachments
	}
	
	// Create test files
	var testFiles [][]byte
	var encrypted [][]byte
	
	for _, size := range fileSizes {
		for i := 0; i < 20; i++ { // 20 files of each size
			data := make([]byte, size)
			rand.Read(data)
			testFiles = append(testFiles, data)
			
			enc, err := provider.EncryptData(data, key)
			if err != nil {
				b.Fatal(err)
			}
			encrypted = append(encrypted, enc)
		}
	}
	
	totalSize := 0
	for _, data := range testFiles {
		totalSize += len(data)
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(totalSize))
	
	for i := 0; i < b.N; i++ {
		for j := range encrypted {
			_, err := provider.DecryptData(encrypted[j], key)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}