package testdata

// TestVector contains known input/output pairs for testing.
type TestVector struct {
	Name       string
	Email      string
	Password   string
	Salt       string // Base64
	Key        string // Hex
	Plaintext  string
	Ciphertext string // Hex
	Path       string
	EncPath    string // Hex
}

// Vectors contains test vectors for crypto operations.
var Vectors = []TestVector{
	{
		Name:      "Basic encryption test",
		Email:     "test@example.com",
		Password:  "testpassword123",
		Salt:      "dGVzdHNhbHQxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Ng==", // 32 bytes
		Key:       "2c3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e",
		Plaintext: "Hello, World!",
		// Note: Actual ciphertext will vary due to random nonce
		Path:    "notes/test.md",
		EncPath: "6e6f7465732f746573742e6d64",
	},
	{
		Name:      "Unicode content test",
		Email:     "user@example.org",
		Password:  "пароль123", // Unicode password
		Salt:      "YW5vdGhlcnNhbHQxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMw==",
		Key:       "3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e",
		Plaintext: "Hello, 世界! 🌍",
		Path:      "daily/2024-01-15.md",
		EncPath:   "6461696c792f323032342d30312d31352e6d64",
	},
}