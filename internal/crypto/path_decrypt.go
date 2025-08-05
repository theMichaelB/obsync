package crypto

import (
	"path"
	"strings"
)

// PathDecryptor handles hierarchical path decryption.
type PathDecryptor struct {
	provider Provider
	cache    map[string]string // Cache decrypted paths
}

// NewPathDecryptor creates a path decryptor with caching.
func NewPathDecryptor(provider Provider) *PathDecryptor {
	return &PathDecryptor{
		provider: provider,
		cache:    make(map[string]string),
	}
}

// DecryptPath decrypts a full file path with caching.
func (pd *PathDecryptor) DecryptPath(hexPath string, vaultKey []byte) (string, error) {
	// Check cache
	if cached, ok := pd.cache[hexPath]; ok {
		return cached, nil
	}
	
	// Decrypt
	plainPath, err := pd.provider.DecryptPath(hexPath, vaultKey)
	if err != nil {
		return "", err
	}
	
	// Normalize path
	plainPath = path.Clean(plainPath)
	plainPath = strings.ReplaceAll(plainPath, "\\", "/")
	
	// Cache result
	pd.cache[hexPath] = plainPath
	
	return plainPath, nil
}

// ClearCache removes all cached paths.
func (pd *PathDecryptor) ClearCache() {
	pd.cache = make(map[string]string)
}