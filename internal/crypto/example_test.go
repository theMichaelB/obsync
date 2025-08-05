package crypto_test

import (
	"encoding/hex"
	"fmt"

	"github.com/yourusername/obsync/internal/crypto"
)

func ExampleProvider_DeriveKey() {
	provider := crypto.NewProvider()

	info := crypto.VaultKeyInfo{
		Version:           1,
		EncryptionVersion: 3,
		Salt:             "dGVzdHNhbHQxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Ng==",
	}

	key, err := provider.DeriveKey("user@example.com", "mypassword", info)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Key length: %d bytes\n", len(key))
	// Output: Key length: 32 bytes
}

func ExampleProvider_DecryptData() {
	provider := crypto.NewProvider()

	// In practice, this key comes from DeriveKey
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i % 256)
	}

	// First encrypt some data to have valid ciphertext
	plaintext := []byte("Hello, World!")
	ciphertext, err := crypto.EncryptData(plaintext, key)
	if err != nil {
		fmt.Printf("Encryption failed: %v\n", err)
		return
	}

	// Now decrypt it
	decrypted, err := provider.DecryptData(ciphertext, key)
	if err != nil {
		fmt.Printf("Decryption failed: %v\n", err)
		return
	}

	fmt.Printf("Decrypted: %s\n", decrypted)
	// Output: Decrypted: Hello, World!
}

func ExampleEncryptData() {
	// Create a test key
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i % 256)
	}

	plaintext := []byte("Secret message")
	ciphertext, err := crypto.EncryptData(plaintext, key)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Encrypted length: %d bytes\n", len(ciphertext))
	fmt.Printf("Format includes nonce (%d) + tag (%d) + data\n", 
		crypto.NonceSize, crypto.TagSize)
	// Output: Encrypted length: 42 bytes
	// Format includes nonce (12) + tag (16) + data
}

func ExamplePathDecryptor() {
	provider := crypto.NewProvider()
	decryptor := crypto.NewPathDecryptor(provider)

	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i % 256)
	}

	// This example shows the interface, though it will fail
	// without proper encrypted path data
	hexPath := hex.EncodeToString([]byte("example"))
	
	_, err := decryptor.DecryptPath(hexPath, key)
	if err != nil {
		fmt.Printf("Decryption failed as expected: path format error\n")
	}
	
	// Clear cache
	decryptor.ClearCache()
	fmt.Printf("Cache cleared\n")
	// Output: Decryption failed as expected: path format error
	// Cache cleared
}

func ExampleValidateKeySize() {
	// Valid key
	validKey := make([]byte, crypto.KeySize)
	err := crypto.ValidateKeySize(validKey)
	fmt.Printf("Valid key error: %v\n", err)

	// Invalid key
	invalidKey := make([]byte, 16) // Too short
	err = crypto.ValidateKeySize(invalidKey)
	fmt.Printf("Invalid key error: %v\n", err != nil)
	// Output: Valid key error: <nil>
	// Invalid key error: true
}