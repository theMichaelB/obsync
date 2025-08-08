package creds

import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    awsconfig "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Combined represents the combined JSON credential model.
type Combined struct {
    Auth struct {
        Email      string `json:"email"`
        Password   string `json:"password"`
        TOTPSecret string `json:"totp_secret"`
    } `json:"auth"`
    Vaults json.RawMessage `json:"vaults"`
}

// ParseCombined parses JSON bytes into Combined.
func ParseCombined(data []byte) (*Combined, error) {
    var c Combined
    if err := json.Unmarshal(data, &c); err != nil {
        return nil, err
    }
    return &c, nil
}

// LoadFromFile loads Combined from a local file path.
func LoadFromFile(path string) (*Combined, error) {
    b, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    return ParseCombined(b)
}

// LoadFromSecret loads Combined from Secrets Manager by name or ARN.
func LoadFromSecret(ctx context.Context, secretID string) (*Combined, error) {
    cfg, err := awsconfig.LoadDefaultConfig(ctx)
    if err != nil {
        return nil, fmt.Errorf("aws config: %w", err)
    }
    sm := secretsmanager.NewFromConfig(cfg)
    out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &secretID})
    if err != nil {
        return nil, fmt.Errorf("get secret value: %w", err)
    }
    if out.SecretString == nil {
        return nil, fmt.Errorf("secret has no string payload")
    }
    return ParseCombined([]byte(*out.SecretString))
}

// VaultPassword returns the vault password for an ID (supports nested or flat maps).
func (c *Combined) VaultPassword(vaultID string) string {
    if len(c.Vaults) == 0 {
        return ""
    }
    // nested format
    var nested map[string]struct{ Password string `json:"password"` }
    if err := json.Unmarshal(c.Vaults, &nested); err == nil {
        if v, ok := nested[vaultID]; ok {
            return v.Password
        }
    }
    // flat format
    var flat map[string]string
    if err := json.Unmarshal(c.Vaults, &flat); err == nil {
        if pw, ok := flat[vaultID]; ok {
            return pw
        }
    }
    return ""
}

