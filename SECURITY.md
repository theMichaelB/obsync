# Security Guidelines

## ⚠️ Credential Management

### Setup Instructions

1. **Start from the minimal config:**
   ```bash
   cp config.min.json config.json
   ```

2. **Edit config.json with your credentials (all required in the minimal file):**
   - Never commit this file to git
   - Fields: `auth.email`, `auth.password`, `auth.totp_secret`
   - Use strong, unique passwords and enable 2FA/TOTP

3. **Alternative: Use environment variables:**
   ```bash
   export OBSIDIAN_EMAIL="your-email@example.com"
   export OBSIDIAN_PASSWORD="your-password"
   export OBSIDIAN_TOTP_SECRET="your-totp-secret"
   ```

### Protected Files

The following files are automatically excluded from git:

- `config.json` - Main configuration with credentials
- `obsync.json` - Alternative config file
- `credentials/` - Entire credentials directory
- `*.key`, `*.pem` - Cryptographic keys
- `token.json` - Authentication tokens
- Any file containing `*secret*`, `*password*`, `*credential*`

### Security Best Practices

- ✅ Use template files for examples
- ✅ Store real credentials locally only
- ✅ Use environment variables in CI/CD
- ✅ Enable 2FA on your Obsidian account
- ✅ Rotate passwords regularly

- ❌ Never commit real credentials
- ❌ Don't share credential files
- ❌ Don't use weak passwords
- ❌ Don't disable TOTP/MFA

### Reporting Security Issues

If you discover a security vulnerability, please report it privately to the maintainers rather than opening a public issue.

## Cryptographic Security

This application uses:

- **AES-256-GCM** for vault encryption/decryption
- **PBKDF2** with 100,000 iterations for key derivation
- **HTTPS + HTTP/2** for secure API communication
- **Constant-time comparison** for authentication verification

## Network Security

- All API communication uses HTTPS
- Proper certificate validation
- Retry logic with exponential backoff
- Request/response logging (credentials are filtered)

## File System Security

- Restrictive permissions (0600) on sensitive files
- Atomic file operations to prevent corruption
- Path traversal protection
- Input sanitization for all file operations
