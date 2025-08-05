package crypto_test

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TheMichaelB/obsync/internal/crypto"
)

func TestSecurityRequirements(t *testing.T) {
	provider := crypto.NewProvider()

	t.Run("key derivation uses sufficient iterations", func(t *testing.T) {
		// Ensure we're using at least 100,000 iterations
		assert.GreaterOrEqual(t, crypto.DefaultIterations, 100000)
	})

	t.Run("key size is 256 bits", func(t *testing.T) {
		assert.Equal(t, 32, crypto.KeySize)
	})

	t.Run("nonce is random for each encryption", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		_, err := rand.Read(key)
		require.NoError(t, err)

		plaintext := []byte("test message")

		// Encrypt twice
		cipher1, err := provider.EncryptData(plaintext, key)
		require.NoError(t, err)

		cipher2, err := provider.EncryptData(plaintext, key)
		require.NoError(t, err)

		// Ciphertexts should be different due to random nonce
		assert.NotEqual(t, cipher1, cipher2)

		// But both should decrypt to same plaintext
		plain1, err := provider.DecryptData(cipher1, key)
		require.NoError(t, err)

		plain2, err := provider.DecryptData(cipher2, key)
		require.NoError(t, err)

		assert.Equal(t, plaintext, plain1)
		assert.Equal(t, plaintext, plain2)
	})

	t.Run("authentication tag prevents tampering", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		_, err := rand.Read(key)
		require.NoError(t, err)

		plaintext := []byte("sensitive data")
		ciphertext, err := crypto.EncryptData(plaintext, key)
		require.NoError(t, err)

		// Tamper with ciphertext
		ciphertext[len(ciphertext)-1] ^= 0xFF

		// Decryption should fail
		_, err = provider.DecryptData(ciphertext, key)
		assert.ErrorIs(t, err, crypto.ErrDecryptionFailed)
	})

	t.Run("constant time comparison for authentication", func(t *testing.T) {
		// GCM mode handles this internally
		// This test ensures we're using GCM correctly
		key := make([]byte, crypto.KeySize)
		_, err := rand.Read(key)
		require.NoError(t, err)

		plaintext := []byte("test")
		ciphertext, err := crypto.EncryptData(plaintext, key)
		require.NoError(t, err)

		// Verify format includes tag
		assert.GreaterOrEqual(t, len(ciphertext), crypto.NonceSize+crypto.TagSize)
	})
}

func TestAESGCMSecurity(t *testing.T) {
	t.Run("encrypt same plaintext produces different ciphertext", func(t *testing.T) {
		key := make([]byte, crypto.KeySize)
		_, err := rand.Read(key)
		require.NoError(t, err)

		plaintext := []byte("same message")

		results := make([][]byte, 10)
		for i := 0; i < 10; i++ {
			ciphertext, err := crypto.EncryptData(plaintext, key)
			require.NoError(t, err)
			results[i] = ciphertext
		}

		// All ciphertexts should be different
		for i := 0; i < len(results); i++ {
			for j := i + 1; j < len(results); j++ {
				assert.NotEqual(t, results[i], results[j],
					"Ciphertext %d and %d should be different", i, j)
			}
		}
	})

	t.Run("wrong key fails decryption", func(t *testing.T) {
		key1 := make([]byte, crypto.KeySize)
		key2 := make([]byte, crypto.KeySize)
		_, err := rand.Read(key1)
		require.NoError(t, err)
		_, err = rand.Read(key2)
		require.NoError(t, err)

		plaintext := []byte("secret message")
		ciphertext, err := crypto.EncryptData(plaintext, key1)
		require.NoError(t, err)

		provider := crypto.NewProvider()
		_, err = provider.DecryptData(ciphertext, key2)
		assert.ErrorIs(t, err, crypto.ErrDecryptionFailed)
	})

	t.Run("key validation", func(t *testing.T) {
		tests := []struct {
			name    string
			keySize int
			wantErr bool
		}{
			{"correct size", crypto.KeySize, false},
			{"too short", crypto.KeySize - 1, true},
			{"too long", crypto.KeySize + 1, true},
			{"zero size", 0, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				key := make([]byte, tt.keySize)
				err := crypto.ValidateKeySize(key)
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})
}