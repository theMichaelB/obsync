package crypto_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourusername/obsync/internal/crypto"
	"github.com/yourusername/obsync/internal/crypto/testdata"
)

func TestProvider_DeriveKey(t *testing.T) {
	provider := crypto.NewProvider()

	tests := []struct {
		name     string
		email    string
		password string
		info     crypto.VaultKeyInfo
		wantErr  bool
	}{
		{
			name:     "valid credentials",
			email:    "test@example.com",
			password: "password123",
			info: crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: crypto.EncryptionVersion,
				Salt:             "dGVzdHNhbHQxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Ng==",
			},
		},
		{
			name:     "unicode password",
			email:    "user@example.com",
			password: "пароль123",
			info: crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: crypto.EncryptionVersion,
				Salt:             "dGVzdHNhbHQxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Ng==",
			},
		},
		{
			name:     "invalid encryption version",
			email:    "test@example.com",
			password: "password123",
			info: crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: 999,
				Salt:             "dGVzdHNhbHQxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Ng==",
			},
			wantErr: true,
		},
		{
			name:     "invalid salt",
			email:    "test@example.com",
			password: "password123",
			info: crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: crypto.EncryptionVersion,
				Salt:             "invalid-base64!",
			},
			wantErr: true,
		},
		{
			name:     "short salt",
			email:    "test@example.com",
			password: "password123",
			info: crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: crypto.EncryptionVersion,
				Salt:             "c2hvcnQ=", // "short" in base64
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := provider.DeriveKey(tt.email, tt.password, tt.info)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, key, crypto.KeySize)

			// Verify deterministic
			key2, err := provider.DeriveKey(tt.email, tt.password, tt.info)
			require.NoError(t, err)
			assert.Equal(t, key, key2)
		})
	}
}

func TestProvider_DecryptData(t *testing.T) {
	provider := crypto.NewProvider()

	t.Run("decrypt with valid key and ciphertext", func(t *testing.T) {
		// Create a valid key and encrypt some data
		key := make([]byte, crypto.KeySize)
		for i := range key {
			key[i] = byte(i)
		}

		plaintext := []byte("Hello, World!")
		ciphertext, err := crypto.EncryptData(plaintext, key)
		require.NoError(t, err)

		// Decrypt
		result, err := provider.DecryptData(ciphertext, key)
		require.NoError(t, err)
		assert.Equal(t, plaintext, result)
	})

	t.Run("invalid key size", func(t *testing.T) {
		shortKey := []byte("short")
		ciphertext := make([]byte, crypto.NonceSize+crypto.TagSize+10)

		_, err := provider.DecryptData(ciphertext, shortKey)
		assert.ErrorIs(t, err, crypto.ErrInvalidKey)
	})

	t.Run("ciphertext too short", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		shortCiphertext := []byte("short")

		_, err := provider.DecryptData(shortCiphertext, key)
		assert.ErrorIs(t, err, crypto.ErrInvalidCiphertext)
	})

	t.Run("tampered ciphertext", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		for i := range key {
			key[i] = byte(i)
		}

		plaintext := []byte("sensitive data")
		ciphertext, err := crypto.EncryptData(plaintext, key)
		require.NoError(t, err)

		// Tamper with ciphertext
		ciphertext[len(ciphertext)-1] ^= 0xFF

		_, err = provider.DecryptData(ciphertext, key)
		assert.ErrorIs(t, err, crypto.ErrDecryptionFailed)
	})
}

func TestProvider_DecryptPath(t *testing.T) {
	provider := crypto.NewProvider()

	t.Run("valid path decryption", func(t *testing.T) {
		t.Skip("Skipping until we implement proper path encryption format")
	})

	t.Run("empty path", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		_, err := provider.DecryptPath("", key)
		assert.ErrorIs(t, err, crypto.ErrInvalidPath)
	})

	t.Run("invalid hex path", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		_, err := provider.DecryptPath("invalid-hex!", key)
		assert.Error(t, err)
	})
}

func TestKeyDerivationVectors(t *testing.T) {
	provider := crypto.NewProvider()

	for _, vector := range testdata.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			info := crypto.VaultKeyInfo{
				Version:           1,
				EncryptionVersion: crypto.EncryptionVersion,
				Salt:             vector.Salt,
			}

			key, err := provider.DeriveKey(vector.Email, vector.Password, info)
			require.NoError(t, err)
			assert.Len(t, key, crypto.KeySize)

			// Verify deterministic
			key2, err := provider.DeriveKey(vector.Email, vector.Password, info)
			require.NoError(t, err)
			assert.Equal(t, key, key2)
		})
	}
}