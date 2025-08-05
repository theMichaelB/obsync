package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// EncryptData encrypts plaintext using AES-GCM.
// Returns: nonce || tag || ciphertext
func EncryptData(plaintext, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	
	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	
	// Generate nonce
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	
	// Encrypt
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	
	// Extract tag from end of ciphertext
	tag := ciphertext[len(ciphertext)-TagSize:]
	encrypted := ciphertext[:len(ciphertext)-TagSize]
	
	// Format: nonce || tag || ciphertext
	result := make([]byte, 0, NonceSize+TagSize+len(encrypted))
	result = append(result, nonce...)
	result = append(result, tag...)
	result = append(result, encrypted...)
	
	return result, nil
}

// ValidateKeySize checks if the key is the correct size.
func ValidateKeySize(key []byte) error {
	if len(key) != KeySize {
		return fmt.Errorf("invalid key size: expected %d, got %d", KeySize, len(key))
	}
	return nil
}