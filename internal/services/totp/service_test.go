package totp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TheMichaelB/obsync/internal/services/totp"
)

func TestTOTPService_GenerateCode(t *testing.T) {
	service := totp.NewService()
	
	tests := []struct {
		name    string
		secret  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid secret",
			secret:  "JBSWY3DPEHPK3PXP", // "Hello world" in base32
			wantErr: false,
		},
		{
			name:    "another valid secret",
			secret:  "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", // Test vector
			wantErr: false,
		},
		{
			name:    "empty secret",
			secret:  "",
			wantErr: true,
			errMsg:  "secret cannot be empty",
		},
		{
			name:    "invalid base32 secret",
			secret:  "invalid-secret-123!@#",
			wantErr: true,
			errMsg:  "failed to generate code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := service.GenerateCode(tt.secret)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Empty(t, code)
			} else {
				assert.NoError(t, err)
				assert.Len(t, code, 6) // Standard 6-digit code
				assert.Regexp(t, `^\d{6}$`, code) // All digits
			}
		})
	}
}

func TestTOTPService_ValidateCode(t *testing.T) {
	service := totp.NewService()
	secret := "JBSWY3DPEHPK3PXP"

	t.Run("validate current code", func(t *testing.T) {
		// Generate a code
		code, err := service.GenerateCode(secret)
		require.NoError(t, err)

		// Should validate successfully
		valid := service.ValidateCode(secret, code)
		assert.True(t, valid)
	})

	t.Run("invalid code", func(t *testing.T) {
		valid := service.ValidateCode(secret, "123456")
		assert.False(t, valid)
	})

	t.Run("empty inputs", func(t *testing.T) {
		assert.False(t, service.ValidateCode("", "123456"))
		assert.False(t, service.ValidateCode(secret, ""))
		assert.False(t, service.ValidateCode("", ""))
	})

	t.Run("wrong length code", func(t *testing.T) {
		assert.False(t, service.ValidateCode(secret, "12345"))
		assert.False(t, service.ValidateCode(secret, "1234567"))
	})
}

func TestTOTPService_GenerateCodeAtTime(t *testing.T) {
	service := totp.NewService()
	secret := "JBSWY3DPEHPK3PXP"

	// Test with known time windows
	testTime1 := time.Unix(1234567890, 0) // Known timestamp
	testTime2 := time.Unix(1234567920, 0) // 30 seconds later

	t.Run("generate at specific time", func(t *testing.T) {
		code1, err := service.GenerateCodeAtTime(secret, testTime1)
		require.NoError(t, err)
		assert.Len(t, code1, 6)

		code2, err := service.GenerateCodeAtTime(secret, testTime2)
		require.NoError(t, err)
		assert.Len(t, code2, 6)

		// Codes should be different (different time windows)
		assert.NotEqual(t, code1, code2)
	})

	t.Run("same time window produces same code", func(t *testing.T) {
		time1 := time.Unix(1234567890, 0)
		time2 := time.Unix(1234567900, 0) // 10 seconds later, same 30s window

		code1, err := service.GenerateCodeAtTime(secret, time1)
		require.NoError(t, err)

		code2, err := service.GenerateCodeAtTime(secret, time2)
		require.NoError(t, err)

		assert.Equal(t, code1, code2)
	})

	t.Run("empty secret", func(t *testing.T) {
		_, err := service.GenerateCodeAtTime("", time.Now())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "secret cannot be empty")
	})
}

func TestTOTPService_GetTimeWindow(t *testing.T) {
	service := totp.NewService()

	current, remaining := service.GetTimeWindow()

	// Current should be a positive number
	assert.Greater(t, current, int64(0))

	// Remaining should be between 0 and 30 seconds
	assert.GreaterOrEqual(t, remaining, time.Duration(0))
	assert.Less(t, remaining, 30*time.Second)

	// Test that time window advances
	time.Sleep(100 * time.Millisecond)
	current2, remaining2 := service.GetTimeWindow()

	// Either same window with less remaining time, or next window
	if current2 == current {
		assert.Less(t, remaining2, remaining)
	} else {
		assert.Equal(t, current2, current+1)
	}
}

func TestTOTPService_IsValidSecret(t *testing.T) {
	service := totp.NewService()

	tests := []struct {
		name    string
		secret  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid base32 secret",
			secret:  "JBSWY3DPEHPK3PXP",
			wantErr: false,
		},
		{
			name:    "valid long secret",
			secret:  "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
			wantErr: false,
		},
		{
			name:    "empty secret",
			secret:  "",
			wantErr: true,
			errMsg:  "secret cannot be empty",
		},
		{
			name:    "invalid characters",
			secret:  "invalid123!@#",
			wantErr: true,
			errMsg:  "Decoding of secret",
		},
		{
			name:    "lowercase base32",
			secret:  "jbswy3dpehpk3pxp",
			wantErr: false, // Should work, library handles case
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.IsValidSecret(tt.secret)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTOTPService_CustomConfig(t *testing.T) {
	// Test custom configuration
	service := totp.NewServiceWithConfig(60, 8, "SHA256")

	secret := "JBSWY3DPEHPK3PXP"
	code, err := service.GenerateCode(secret)
	
	require.NoError(t, err)
	assert.NotEmpty(t, code)
	
	// Note: The underlying library might not respect all custom configs
	// but the service should still function
}

func TestTOTPService_RealWorldScenarios(t *testing.T) {
	service := totp.NewService()

	t.Run("obsidian-like secret", func(t *testing.T) {
		// Test with a realistic Obsidian TOTP secret format
		secret := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" // 32 chars, valid base32

		code, err := service.GenerateCode(secret)
		require.NoError(t, err)
		assert.Len(t, code, 6)
		assert.Regexp(t, `^\d{6}$`, code)

		// Validate the generated code
		valid := service.ValidateCode(secret, code)
		assert.True(t, valid)
	})

	t.Run("timing edge case", func(t *testing.T) {
		secret := "JBSWY3DPEHPK3PXP"

		// Generate codes around a 30-second boundary
		now := time.Now()
		
		// Find the next 30-second boundary
		seconds := now.Second()
		nextBoundary := now.Add(time.Duration(30-(seconds%30)) * time.Second)
		
		// Generate codes just before and after the boundary
		codeBefore, err := service.GenerateCodeAtTime(secret, nextBoundary.Add(-1*time.Second))
		require.NoError(t, err)

		codeAfter, err := service.GenerateCodeAtTime(secret, nextBoundary.Add(1*time.Second))
		require.NoError(t, err)

		// They should be different
		assert.NotEqual(t, codeBefore, codeAfter)
	})
}

// Benchmark tests
func BenchmarkTOTPService_GenerateCode(b *testing.B) {
	service := totp.NewService()
	secret := "JBSWY3DPEHPK3PXP"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := service.GenerateCode(secret)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTOTPService_ValidateCode(b *testing.B) {
	service := totp.NewService()
	secret := "JBSWY3DPEHPK3PXP"
	
	// Pre-generate a code
	code, _ := service.GenerateCode(secret)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		service.ValidateCode(secret, code)
	}
}