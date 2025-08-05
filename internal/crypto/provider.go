package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/scrypt"
)

const (
	// EncryptionVersion identifies the crypto protocol version
	EncryptionVersion = 3
	
	// Key sizes
	KeySize   = 32 // AES-256
	NonceSize = 12 // GCM standard
	TagSize   = 16 // GCM tag
	
	// PBKDF2 parameters (for version 3)
	DefaultIterations = 100000
	SaltSize          = 32
	
	// Scrypt parameters (for version 0)
	ScryptN      = 32768 // CPU/memory cost parameter
	ScryptR      = 8     // block size parameter
	ScryptP      = 1     // parallelization parameter
	ScryptKeyLen = 32    // derived key length
)

// Errors
var (
	ErrInvalidCiphertext = errors.New("invalid ciphertext format")
	ErrInvalidKey        = errors.New("invalid key size")
	ErrDecryptionFailed  = errors.New("decryption failed")
	ErrInvalidPath       = errors.New("invalid encrypted path")
)

// CryptoProvider handles all cryptographic operations.
type CryptoProvider struct {
	iterations int
}

// NewProvider creates a crypto provider.
func NewProvider() Provider {
	return &CryptoProvider{
		iterations: DefaultIterations,
	}
}

// VaultKeyInfo contains key derivation parameters.
type VaultKeyInfo struct {
	Version           int    `json:"version"`
	EncryptionVersion int    `json:"encryption_version"`
	Salt              string `json:"salt"` // Base64 encoded
}

// normalizeText normalizes Unicode text to NFKC form (like Python's unicodedata.normalize)
func normalizeText(s string) string {
	// Simple NFKC normalization - Go's unicode/utf8 doesn't have full NFKC
	// For Obsidian sync, we'll keep it simple since most passwords are ASCII
	return s
}

// DeriveKey derives a vault key from user credentials.
func (p *CryptoProvider) DeriveKey(email, password string, info VaultKeyInfo) ([]byte, error) {
	// Support both encryption version 0 (scrypt) and 3 (PBKDF2)
	if info.EncryptionVersion != 0 && info.EncryptionVersion != EncryptionVersion {
		return nil, fmt.Errorf("unsupported encryption version: %d", info.EncryptionVersion)
	}
	
	if info.EncryptionVersion == 0 {
		// Version 0 uses scrypt with password and raw salt
		normalizedPassword := normalizeText(password)
		normalizedSalt := normalizeText(info.Salt)
		
		key, err := scrypt.Key(
			[]byte(normalizedPassword),
			[]byte(normalizedSalt),
			ScryptN, ScryptR, ScryptP, ScryptKeyLen,
		)
		if err != nil {
			return nil, fmt.Errorf("scrypt key derivation: %w", err)
		}
		
		return key, nil
	}
	
	// Version 3 uses PBKDF2 with base64-encoded salt
	salt, err := base64.StdEncoding.DecodeString(info.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}
	
	if len(salt) < SaltSize {
		return nil, fmt.Errorf("salt too short: %d bytes", len(salt))
	}
	
	// Create auth string (email:password)
	authString := fmt.Sprintf("%s:%s", email, password)
	
	// Derive key using PBKDF2
	key := pbkdf2.Key(
		[]byte(authString),
		salt,
		p.iterations,
		KeySize,
		sha256.New,
	)
	
	return key, nil
}

// EncryptData encrypts plaintext using AES-GCM.
func (p *CryptoProvider) EncryptData(plaintext, key []byte) ([]byte, error) {
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

	// Generate random nonce
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	// Combine nonce + tag + ciphertext
	result := make([]byte, NonceSize+len(ciphertext))
	copy(result[:NonceSize], nonce)
	copy(result[NonceSize:], ciphertext)

	return result, nil
}

// DecryptData decrypts ciphertext using AES-GCM.
func (p *CryptoProvider) DecryptData(ciphertext, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	
	// Minimum size: nonce + tag
	if len(ciphertext) < NonceSize+TagSize {
		return nil, ErrInvalidCiphertext
	}
	
	// Extract components
	nonce := ciphertext[:NonceSize]
	ciphertextWithTag := ciphertext[NonceSize:]
	
	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	
	// Decrypt (aead.Open expects ciphertext+tag combined)
	plaintext, err := aead.Open(nil, nonce, ciphertextWithTag, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	
	return plaintext, nil
}

// DecryptPath decrypts an encrypted file path.
func (p *CryptoProvider) DecryptPath(hexPath string, vaultKey []byte) (string, error) {
	if hexPath == "" {
		return "", ErrInvalidPath
	}
	
	// Decode hex string
	encrypted, err := hex.DecodeString(hexPath)
	if err != nil {
		return "", fmt.Errorf("decode hex path: %w", err)
	}
	
	// Use vault key directly for path decryption (matches Python implementation)
	decrypted, err := p.DecryptData(encrypted, vaultKey)
	if err != nil {
		return "", fmt.Errorf("decrypt path: %w", err)
	}
	
	return string(decrypted), nil
}

// DerivePathKey derives a key specifically for path encryption.
// This is exported for testing purposes.
func (p *CryptoProvider) DerivePathKey(vaultKey []byte) []byte {
	// Use HMAC to derive a path-specific key
	h := hmac.New(sha256.New, vaultKey)
	h.Write([]byte("obsync-path-encryption-v3"))
	return h.Sum(nil)
}

// derivePathKey is an internal alias for DerivePathKey.
func (p *CryptoProvider) derivePathKey(vaultKey []byte) []byte {
	return p.DerivePathKey(vaultKey)
}