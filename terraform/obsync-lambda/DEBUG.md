# Lambda Debug Options

## Quick Enable Debug Mode

In `terraform.tfvars`, set:
```hcl
enable_debug         = true   # Verbose logging
save_websocket_trace = true   # Save WebSocket communications
log_level           = "debug" # Maximum verbosity
```

Then apply: `terraform apply`

## Debug Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Logging verbosity: debug, info, warn, error | info |
| `OBSYNC_DEBUG` | Enable debug mode | false |
| `SAVE_WEBSOCKET_TRACE` | Save WebSocket messages to S3 | false |
| `WEBSOCKET_TRACE_BUCKET` | S3 bucket for traces | (auto-set) |
| `WEBSOCKET_TRACE_PREFIX` | S3 prefix for traces | debug/websocket-traces/ |

## Viewing Debug Output

### CloudWatch Logs
```bash
# Stream logs in real-time
aws logs tail /aws/lambda/obsync-sync --follow

# View recent logs
aws logs filter-log-events \
  --log-group-name /aws/lambda/obsync-sync \
  --start-time $(date -u -d '1 hour ago' +%s)000
```

### WebSocket Traces (when enabled)
```bash
# List trace files
aws s3 ls s3://obsync-BUCKET_ID/debug/websocket-traces/

# Download trace
aws s3 cp s3://obsync-BUCKET_ID/debug/websocket-traces/TRACE_FILE.json .
```

## Manual Debug Invocation

```bash
# Test with debug output
aws lambda invoke \
  --function-name obsync-sync \
  --payload '{"action": "sync", "sync_type": "incremental"}' \
  --log-type Tail \
  response.json | jq -r '.LogResult' | base64 -d
```

## Common Debug Scenarios

### 1. Authentication Issues
Enable debug to see:
- Credential loading from Secrets Manager
- Token refresh attempts
- API authentication responses

### 2. Sync Failures
Debug mode shows:
- Vault listing and selection
- File comparison logic
- Download progress and errors
- Decryption attempts

### 3. Performance Issues
Check for:
- Memory usage warnings
- Timeout approaching messages
- Batch processing sizes
- S3 operation durations

### 4. WebSocket Communication
With `save_websocket_trace = true`:
- All WebSocket messages saved to S3
- Includes raw protocol messages
- Useful for protocol debugging

## Troubleshooting Tips

1. **No vaults syncing**: Check credentials in Secrets Manager
2. **Timeout errors**: Increase `timeout` in terraform.tfvars
3. **Memory errors**: Increase `memory_size` 
4. **Permission errors**: Check IAM role has secretsmanager:GetSecretValue

## Disable Debug (Production)

Remember to disable debug features for production:
```hcl
enable_debug         = false
save_websocket_trace = false
log_level           = "info"
```