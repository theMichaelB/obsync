package handler

import (
    "context"
    "encoding/json"
    "fmt"

    awsconfig "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// combinedSecret models a single JSON secret that contains both
// account credentials and per-vault passwords.
// Example:
// {
//   "auth": {"email": "...", "password": "...", "totp_secret": "..."},
//   "vaults": {"vault-abc": {"password": "pw"}, "vault-xyz": {"password": "pw2"}}
// }
// Also supports a flat vaults map: "vaults": {"vault-abc": "pw"}
type combinedSecret struct {
    Auth struct {
        Email      string `json:"email"`
        Password   string `json:"password"`
        TOTPSecret string `json:"totp_secret"`
    } `json:"auth"`
    Vaults json.RawMessage `json:"vaults"`
}

// secretVaultMap captures nested or flat vault password formats
type secretVaultMap struct {
    Nested map[string]struct{ Password string `json:"password"` }
    Flat   map[string]string
}

func parseVaults(raw json.RawMessage) (map[string]string, error) {
    if len(raw) == 0 {
        return map[string]string{}, nil
    }
    // try nested first
    var nested map[string]struct{ Password string `json:"password"` }
    if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 {
        out := make(map[string]string, len(nested))
        for k, v := range nested {
            out[k] = v.Password
        }
        return out, nil
    }
    // try flat
    var flat map[string]string
    if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 {
        return flat, nil
    }
    return map[string]string{}, nil
}

func (h *Handler) loadCombinedSecret(ctx context.Context, secretID string) error {
    if secretID == "" {
        return nil
    }
    cfg, err := awsconfig.LoadDefaultConfig(ctx)
    if err != nil {
        return fmt.Errorf("aws config: %w", err)
    }
    sm := secretsmanager.NewFromConfig(cfg)
    out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &secretID})
    if err != nil {
        return fmt.Errorf("get secret value: %w", err)
    }
    if out.SecretString == nil {
        return fmt.Errorf("secret has no string payload")
    }
    var cs combinedSecret
    if err := json.Unmarshal([]byte(*out.SecretString), &cs); err != nil {
        return fmt.Errorf("parse secret json: %w", err)
    }
    // apply to handler fields for later use
    if cs.Auth.Email != "" {
        h.clientAuthEmail = cs.Auth.Email
        h.clientAuthPassword = cs.Auth.Password
        h.clientAuthTOTP = cs.Auth.TOTPSecret
    }
    vaults, err := parseVaults(cs.Vaults)
    if err != nil {
        return fmt.Errorf("parse vaults: %w", err)
    }
    if h.vaultPasswords == nil {
        h.vaultPasswords = map[string]string{}
    }
    for id, pw := range vaults {
        h.vaultPasswords[id] = pw
    }
    return nil
}

