# Obsync Lambda Configuration
aws_region      = "us-east-1"
function_name   = "obsync-sync"
memory_size     = 1024
timeout         = 900

# Enable scheduled sync every hour
enable_schedule     = true
schedule_expression = "rate(1 hour)"

# S3 storage configuration
s3_prefix       = "vaults/"
s3_state_prefix = "state/"

# Debug options (set to true for troubleshooting)
enable_debug         = false  # Enable debug logging
save_websocket_trace = false  # Save WebSocket traces to S3
log_level           = "info"  # Options: debug, info, warn, error

# Performance tuning
max_concurrent = 50  # Concurrent file operations (increase for faster sync)
chunk_size_mb  = 10  # File chunk size in MB

# REQUIRED: Create this secret first with:
# aws secretsmanager create-secret \
#   --name obsync-credentials \
#   --secret-string file://credentials.json
#
# Then add the ARN here:
secrets_manager_secret_arn = "arn:aws:secretsmanager:us-east-1:YOUR_ACCOUNT_ID:secret:obsync-credentials-XXXXX"