# Obsync - Secure Obsidian Vault Synchronization

**Download your Obsidian vaults securely to any machine** - Obsync is a powerful command-line tool that provides secure, one-way synchronization of your Obsidian vaults with enterprise-grade encryption and authentication.

## 🚀 Quick Start

### 1. Install Obsync
```bash
# Download the latest release or build from source
go build -o obsync ./cmd/obsync
```

### 2. Set Up Your Credentials
```bash
# Copy the config template
cp config.template.json config.json

# Edit config.json with your Obsidian account details
```

### 3. Sync Your Vault
```bash
# Login to your Obsidian account
./obsync login

# List your vaults
./obsync vaults

# Download a vault
./obsync sync vault-id --dest ./my-vault
```

## ✨ Key Features

### 🔐 **Enterprise-Grade Security**
- **AES-256-GCM encryption** for all vault data
- **TOTP/2FA authentication** with automatic code generation
- **Secure credential storage** with multiple protection layers
- **HTTPS/HTTP2** for all network communications

### 📦 **Smart Synchronization**
- **Incremental syncs** - Download only what's changed
- **Resume capability** - Pick up where you left off after interruptions
- **Dry-run mode** - Preview changes before downloading
- **Progress tracking** - Real-time sync status with file counts

### 🛠️ **Flexible Configuration**
- **JSON config files** for persistent settings
- **Environment variables** for CI/CD integration
- **Command-line flags** for one-time overrides
- **Multiple output formats** (text, JSON, tables)

### ☁️ **Serverless Support (New!)**
- **AWS Lambda deployment** for automated, scheduled syncs
- **S3 storage backend** for cloud-native architectures
- **DynamoDB state management** for distributed systems
- **Auto-scaling** based on vault size and complexity

## 📖 Complete Command Reference

### Authentication & Access

#### Login to Obsidian
```bash
# Interactive login (prompts for password)
./obsync login --email your@email.com

# With password (not recommended - use config file instead)
./obsync login --email your@email.com --password yourpass

# With TOTP code
./obsync login --email your@email.com --totp 123456
```

#### Generate TOTP Codes
```bash
# Generate current code
./obsync totp

# Watch mode - auto-refresh every 30 seconds
./obsync totp --watch

# Use custom secret
./obsync totp --secret "YOUR_BASE32_SECRET"

# JSON output for scripting
./obsync totp --json
```

### Vault Management

#### List Your Vaults
```bash
# Show all vaults
./obsync vaults

# JSON output for automation
./obsync vaults --json

# Detailed table view
./obsync vaults --format table
```

#### Sync a Vault
```bash
# Basic sync
./obsync sync vault-id --dest ./local-folder

# Full sync (ignore local state)
./obsync sync vault-id --dest ./local-folder --full

# Dry run - see what would change
./obsync sync vault-id --dest ./local-folder --dry-run

# With vault password
./obsync sync vault-id --dest ./local-folder --password "vault-pass"
```

#### Check Sync Status
```bash
# Show status for all vaults
./obsync status

# JSON output
./obsync status --json

# Specific vault
./obsync status vault-id
```

#### Reset Sync State
```bash
# Reset specific vault
./obsync reset vault-id

# Reset all vaults
./obsync reset --all

# Force reset without confirmation
./obsync reset vault-id --force
```

### Configuration Management

#### View Configuration
```bash
# Show current config (hides sensitive data)
./obsync config show

# Generate example config
./obsync config example

# Debug config resolution
./obsync debug config
```

### Debugging & Troubleshooting

```bash
# Show authentication token details
./obsync debug token

# View sync state for a vault
./obsync debug state vault-id

# Test authentication
./obsync auth-test verify
```

## ⚙️ Configuration Options

### Config File (config.json)
```json
{
  "auth": {
    "email": "your@email.com",
    "password": "your-password",
    "totp_secret": "YOUR_BASE32_SECRET",
    "token_file": ".obsync/auth/token.json"
  },
  "storage": {
    "data_dir": ".obsync/vaults",
    "state_dir": ".obsync/state",
    "temp_dir": ".obsync/temp"
  },
  "sync": {
    "max_concurrent": 5,
    "chunk_size": 1048576,
    "validate_checksums": true
  },
  "log": {
    "level": "info",
    "format": "text"
  }
}
```

### Environment Variables
```bash
# Authentication
export OBSIDIAN_EMAIL="your@email.com"
export OBSIDIAN_PASSWORD="your-password"
export OBSIDIAN_TOTP_SECRET="YOUR_BASE32_SECRET"

# Storage paths
export OBSYNC_DATA_DIR="/path/to/data"
export OBSYNC_STATE_DIR="/path/to/state"

# Logging
export OBSYNC_LOG_LEVEL="debug"
export OBSYNC_LOG_FORMAT="json"
```

## 🔒 Security Best Practices

1. **Never commit credentials** - config.json is gitignored by default
2. **Use TOTP/2FA** - Enable two-factor authentication on your Obsidian account
3. **Secure your config** - Set file permissions to 0600 on config.json
4. **Use environment variables** in CI/CD pipelines
5. **Rotate credentials regularly** - Update passwords and TOTP secrets periodically

See [SECURITY.md](SECURITY.md) for comprehensive security guidelines.

## 🚢 AWS Lambda Deployment

Obsync can run serverless on AWS Lambda for automated, scheduled syncs:

```bash
# Build Lambda deployment package
make build-lambda

# Deploy (requires AWS CLI configured)
aws lambda create-function \
  --function-name obsync-sync \
  --runtime provided.al2 \
  --handler bootstrap \
  --zip-file fileb://build/obsync-lambda.zip \
  --environment Variables="{
    OBSIDIAN_EMAIL=your@email.com,
    OBSIDIAN_PASSWORD=yourpass,
    S3_BUCKET=my-vault-bucket,
    STATE_TABLE_NAME=obsync-state
  }"
```

Lambda features:
- Memory-aware processing with automatic throttling
- Batch processing for large vaults
- Progress tracking across invocations
- Automatic retry with error recovery

## 🏗️ Architecture

Obsync is built with clean architecture principles:

- **Interface-based design** - Easy to extend and test
- **Modular packages** - Separated concerns for maintainability
- **Comprehensive testing** - Unit, integration, and race condition tests
- **Security-first** - Encryption, validation, and safe credential handling

### Core Components

- **Transport Layer** - HTTP/2 client with WebSocket support
- **Crypto Provider** - AES-256-GCM encryption with PBKDF2
- **State Management** - SQLite for local, DynamoDB for Lambda
- **Storage Adapters** - Local filesystem or S3
- **Services** - Auth, Vaults, Sync orchestration

## 🧪 Development

### Building from Source
```bash
# Clone the repository
git clone https://github.com/yourusername/obsync.git
cd obsync

# Install dependencies
go mod download

# Build the binary
make build

# Run tests
make test

# Run with race detection
go test -race ./...
```

### Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes with tests
4. Run `make test` and `make lint`
5. Commit your changes
6. Push to your fork and open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🤝 Support

- **Issues**: [GitHub Issues](https://github.com/yourusername/obsync/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/obsync/discussions)
- **Security**: See [SECURITY.md](SECURITY.md) for reporting vulnerabilities

## 🙏 Acknowledgments

Built with Go and powered by:
- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [AWS SDK](https://aws.amazon.com/sdk-for-go/) - Lambda support
- [Logrus](https://github.com/sirupsen/logrus) - Structured logging
- The Obsidian community for inspiration

---

**Note**: Obsync is an independent project and is not affiliated with or endorsed by Obsidian.