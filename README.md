# Obsync - Secure Obsidian Vault Synchronization

A secure, one-way synchronization tool for Obsidian vaults written in Go.

## ⚠️ Quick Setup (IMPORTANT)

1. **Copy the template config:**
   ```bash
   cp config.template.json config.json
   ```

2. **Edit config.json with your credentials:**
   - Your Obsidian account email
   - Your Obsidian account password  
   - Your TOTP secret from Obsidian settings

3. **Never commit your credentials:**
   - `config.json` is automatically excluded from git
   - See [SECURITY.md](SECURITY.md) for full security guidelines

## Features

- 🔐 AES-256-GCM encryption with PBKDF2 key derivation
- 🔗 WebSocket-based incremental sync protocol
- 🌐 HTTP/2 REST API client with retry logic
- 📁 Atomic file operations with conflict resolution
- 💾 SQLite-based state persistence
- 🎯 UID-based deterministic sync
- ✅ Full TOTP/MFA authentication support

## Usage

```bash
# Login to Obsidian
./obsync login --email="your-email" --password="your-password" --totp="123456"

# List available vaults
./obsync vaults list

# Sync a vault
./obsync sync vault-id --dest ./my-vault

# Check sync status
./obsync status
```

## Environment Variables

You can use environment variables instead of config files:

```bash
export OBSIDIAN_EMAIL="your-email@example.com"
export OBSIDIAN_PASSWORD="your-password"
export OBSIDIAN_TOTP_SECRET="your-totp-secret"
```

## Security

See [SECURITY.md](SECURITY.md) for comprehensive security guidelines and best practices.

## Architecture

- **Clean Architecture**: Interface-based design with dependency injection
- **Modular Packages**: config, crypto, models, services, state, storage, transport
- **Comprehensive Testing**: 87/87 core tests passing with race condition detection
- **Security First**: Proper credential handling, encryption, and input validation

## Development

```bash
# Build
go build -o obsync ./cmd/obsync

# Run tests
go test ./internal/...

# Run with race detection
go test -race ./internal/...

# Linting
golangci-lint run
```