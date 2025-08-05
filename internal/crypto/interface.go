package crypto

// Provider defines the interface for cryptographic operations.
type Provider interface {
	// DeriveKey derives a vault key from user credentials.
	DeriveKey(email, password string, info VaultKeyInfo) ([]byte, error)
	
	// EncryptData encrypts plaintext using AES-GCM.
	EncryptData(plaintext, key []byte) ([]byte, error)
	
	// DecryptData decrypts ciphertext using AES-GCM.
	DecryptData(ciphertext, key []byte) ([]byte, error)
	
	// DecryptPath decrypts an encrypted file path.
	DecryptPath(hexPath string, vaultKey []byte) (string, error)
	
	// DerivePathKey derives a key specifically for path encryption.
	DerivePathKey(vaultKey []byte) []byte
}