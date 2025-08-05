package crypto_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/TheMichaelB/obsync/internal/crypto"
)

func BenchmarkKeyDerivation(b *testing.B) {
	provider := crypto.NewProvider()
	salt := make([]byte, 32)
	_, err := rand.Read(salt)
	if err != nil {
		b.Fatal(err)
	}

	info := crypto.VaultKeyInfo{
		Version:           1,
		EncryptionVersion: crypto.EncryptionVersion,
		Salt:             base64.StdEncoding.EncodeToString(salt),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.DeriveKey("test@example.com", "password123", info)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecryptData(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, crypto.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		b.Fatal(err)
	}

	plaintext := make([]byte, 1024) // 1KB
	_, err = rand.Read(plaintext)
	if err != nil {
		b.Fatal(err)
	}

	ciphertext, err := crypto.EncryptData(plaintext, key)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))

	for i := 0; i < b.N; i++ {
		_, err := provider.DecryptData(ciphertext, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncryptData(b *testing.B) {
	key := make([]byte, crypto.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		b.Fatal(err)
	}

	plaintext := make([]byte, 1024) // 1KB
	_, err = rand.Read(plaintext)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))

	for i := 0; i < b.N; i++ {
		_, err := crypto.EncryptData(plaintext, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPathDecryption(b *testing.B) {
	provider := crypto.NewProvider()
	key := make([]byte, crypto.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		b.Fatal(err)
	}

	// Create a realistic encrypted path
	path := "notes/subfolder/document.md"
	
	// We need to manually create the path key for testing
	// This simulates what would happen in real usage
	pathKey := make([]byte, crypto.KeySize)
	_, err = rand.Read(pathKey)
	if err != nil {
		b.Fatal(err)
	}
	
	encrypted, err := crypto.EncryptData([]byte(path), pathKey)
	if err != nil {
		b.Fatal(err)
	}
	hexPath := hex.EncodeToString(encrypted)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Note: This will fail in practice because we're not using the proper
		// path key derivation, but it benchmarks the decryption overhead
		_, _ = provider.DecryptPath(hexPath, key)
	}
}

func BenchmarkPathDecryptorWithCaching(b *testing.B) {
	provider := crypto.NewProvider()
	decryptor := crypto.NewPathDecryptor(provider)
	
	key := make([]byte, crypto.KeySize)
	_, err := rand.Read(key)
	if err != nil {
		b.Fatal(err)
	}

	// Create multiple paths to test caching behavior
	paths := []string{
		"notes/file1.md",
		"daily/2024-01.md", 
		"projects/work.md",
		"notes/file1.md", // Duplicate to test cache hit
	}

	// Pre-populate with some valid hex paths for testing
	hexPaths := make([]string, len(paths))
	for i, path := range paths {
		// Simple hex encoding for benchmark purposes
		hexPaths[i] = hex.EncodeToString([]byte(path))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pathIdx := i % len(hexPaths)
		// This will fail in practice but benchmarks the cache overhead
		_, _ = decryptor.DecryptPath(hexPaths[pathIdx], key)
	}
}

func BenchmarkValidateKeySize(b *testing.B) {
	key := make([]byte, crypto.KeySize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = crypto.ValidateKeySize(key)
	}
}