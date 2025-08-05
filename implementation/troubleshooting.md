# Obsync Troubleshooting Guide

This guide helps diagnose and resolve common issues with the Obsync vault synchronization client.

## Table of Contents

1. [Authentication Issues](#authentication-issues)
2. [Connection Problems](#connection-problems)
3. [Sync Failures](#sync-failures)
4. [Decryption Errors](#decryption-errors)
5. [File System Issues](#file-system-issues)
6. [Performance Problems](#performance-problems)
7. [State Corruption](#state-corruption)
8. [Platform-Specific Issues](#platform-specific-issues)

## Authentication Issues

### Problem: "Login failed: 401 Unauthorized"

**Symptoms:**
```
❌ Login failed: API error 401 (AUTH_ERROR): Invalid credentials
```

**Causes:**
- Incorrect email or password
- Account locked or suspended
- Expired credentials

**Solutions:**
1. Verify email and password:
   ```bash
   obsync login --email your@email.com
   ```
2. Check for typos (email is case-insensitive, password is not)
3. Try resetting password through web interface
4. Check if 2FA is required:
   ```bash
   obsync login --email your@email.com --totp 123456
   ```

### Problem: "Token expired" during sync

**Symptoms:**
```
Error: not authenticated: token expired
```

**Solutions:**
1. Re-login to refresh token:
   ```bash
   obsync login --email your@email.com
   ```
2. Check token file permissions:
   ```bash
   ls -la ~/.obsync/token.json
   chmod 600 ~/.obsync/token.json
   ```

## Connection Problems

### Problem: "Connection timeout"

**Symptoms:**
```
Error: connect websocket: dial tcp: i/o timeout
```

**Causes:**
- Network connectivity issues
- Firewall blocking connections
- Proxy configuration needed

**Solutions:**
1. Test network connectivity:
   ```bash
   curl -v https://api.obsync.app/health
   ```

2. Check firewall rules for:
   - HTTPS (port 443)
   - WebSocket connections (WSS)

3. Configure proxy if needed:
   ```bash
   export HTTPS_PROXY=http://proxy.company.com:8080
   obsync sync vault-123 --dest ./vault
   ```

4. Increase timeout in config:
   ```json
   {
     "api": {
       "timeout": "60s"
     }
   }
   ```

### Problem: "WebSocket connection lost"

**Symptoms:**
```
WebSocket read error: websocket: close 1006 (abnormal closure)
```

**Solutions:**
1. Check for intermittent network issues
2. Enable debug logging to see disconnection reason:
   ```bash
   obsync --log-level=debug sync vault-123 --dest ./vault
   ```
3. Sync will auto-resume from last position on next run

## Sync Failures

### Problem: "Sync already in progress"

**Symptoms:**
```
Error: sync already in progress
```

**Solutions:**
1. Wait for current sync to complete
2. If sync is stuck, restart obsync
3. Check for orphaned lock files:
   ```bash
   rm ~/.obsync/state/vault-123.lock
   ```

### Problem: "No space left on device"

**Symptoms:**
```
Error: write file: no space left on device
```

**Solutions:**
1. Check disk space:
   ```bash
   df -h /path/to/vault
   ```
2. Clean temporary files:
   ```bash
   rm -rf ~/.obsync/temp/*
   ```
3. Use different destination with more space

### Problem: Files missing after sync

**Causes:**
- Incremental sync skipping unchanged files
- Files deleted on server
- Decryption failures

**Diagnosis:**
```bash
# Check sync state
obsync status

# View detailed sync log
obsync --log-level=debug sync vault-123 --dest ./vault 2>sync.log
grep "File skipped\|File error" sync.log
```

**Solutions:**
1. Force full sync:
   ```bash
   obsync sync vault-123 --dest ./vault --full
   ```
2. Reset sync state:
   ```bash
   obsync reset vault-123
   obsync sync vault-123 --dest ./vault
   ```

## Decryption Errors

### Problem: "Decryption failed"

**Symptoms:**
```
Error: decrypt path: decryption failed
Error: decrypt content: decryption failed
```

**Causes:**
- Wrong vault password
- Corrupted encrypted data
- Unsupported encryption version

**Solutions:**
1. Verify vault password is correct
2. Check encryption version:
   ```bash
   obsync vaults
   # Look for ENCRYPTION column
   ```
3. Try with fresh credentials:
   ```bash
   obsync login --email your@email.com
   obsync sync vault-123 --dest ./vault --password
   ```

### Problem: "Integrity check failed"

**Symptoms:**
```
integrity check failed for notes/file.md: expected abc123, got def456
```

**Causes:**
- File corruption during download
- Man-in-the-middle attack (rare)
- Server-side corruption

**Solutions:**
1. Retry sync (automatic retry should handle transient issues)
2. Check network stability
3. Report persistent integrity failures to support

## File System Issues

### Problem: "Invalid path" errors

**Symptoms:**
```
Error: sanitize path: invalid path: contains '..'
Error: invalid path: contains reserved name 'CON'
```

**Causes:**
- Malformed paths from server
- Platform-specific path restrictions
- Path length limits

**Solutions:**
1. Enable path debugging:
   ```bash
   obsync --log-level=debug sync vault-123 --dest ./vault 2>&1 | grep path
   ```
2. Use shorter destination path
3. On Windows, avoid reserved names (CON, PRN, AUX, etc.)

### Problem: "Permission denied"

**Symptoms:**
```
Error: write file: permission denied
Error: create directory: permission denied
```

**Solutions:**
1. Check destination permissions:
   ```bash
   ls -la /path/to/vault
   chmod -R u+rw /path/to/vault
   ```
2. Run as appropriate user (avoid root/admin unless necessary)
3. On Windows, check if files are read-only

### Problem: Symlink issues

**Symptoms:**
```
Error: symbolic links not supported
```

**Solutions:**
1. Obsync doesn't sync symlinks by default
2. Use real files instead of symlinks
3. Configure symlink handling in future versions

## Performance Problems

### Problem: Sync is very slow

**Diagnosis:**
```bash
# Check sync progress
obsync sync vault-123 --dest ./vault

# Monitor network usage
netstat -i 1  # Linux/Mac
```

**Solutions:**
1. Check network bandwidth
2. Increase concurrent downloads:
   ```json
   {
     "sync": {
       "max_concurrent": 10
     }
   }
   ```
3. Use wired connection instead of WiFi
4. Sync during off-peak hours

### Problem: High memory usage

**Symptoms:**
- System becomes unresponsive
- Out of memory errors

**Solutions:**
1. Limit concurrent operations:
   ```json
   {
     "sync": {
       "max_concurrent": 3,
       "chunk_size": 524288
     }
   }
   ```
2. Monitor memory usage:
   ```bash
   top -p $(pgrep obsync)
   ```

## State Corruption

### Problem: "State file is corrupt"

**Symptoms:**
```
Error: load state: state file is corrupt
```

**Solutions:**
1. Reset state for affected vault:
   ```bash
   obsync reset vault-123
   ```
2. Check for backup state:
   ```bash
   ls ~/.obsync/state/vault-123.json*
   cp ~/.obsync/state/vault-123.json.backup ~/.obsync/state/vault-123.json
   ```
3. Force full sync after reset

### Problem: Sync starts from beginning repeatedly

**Causes:**
- State not saving properly
- Disk full preventing state writes
- Permission issues

**Solutions:**
1. Check state directory permissions:
   ```bash
   ls -la ~/.obsync/state/
   chmod 700 ~/.obsync/state/
   ```
2. Verify disk space for state files
3. Enable state debugging:
   ```bash
   obsync --log-level=debug sync vault-123 --dest ./vault 2>&1 | grep state
   ```

## Platform-Specific Issues

### Windows

**Problem: Long path errors**
```
Error: path too long: 261 characters (max: 260)
```

**Solution:**
1. Enable long path support (Windows 10+):
   - Run as Administrator:
   ```powershell
   New-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\FileSystem" -Name "LongPathsEnabled" -Value 1 -PropertyType DWORD -Force
   ```
2. Use shorter destination path
3. Reduce nesting in vault structure

**Problem: Reserved filename errors**

**Solution:**
Rename files on server to avoid Windows reserved names:
- CON, PRN, AUX, NUL
- COM1-COM9, LPT1-LPT9

### macOS

**Problem: "too many open files"**

**Solution:**
1. Increase file descriptor limit:
   ```bash
   ulimit -n 4096
   ```
2. Reduce concurrent operations in config

### Linux

**Problem: Case sensitivity issues**

**Symptoms:**
Files with names differing only in case cause conflicts

**Solution:**
1. Use case-sensitive file system
2. Rename conflicting files on server
3. Enable case-conflict resolution (future feature)

## Debug Mode

For difficult issues, enable full debugging:

```bash
# Maximum verbosity
obsync --log-level=debug --log-format=json sync vault-123 --dest ./vault 2>debug.log

# Analyze debug log
jq '.msg' debug.log | grep -i error
jq 'select(.level=="error")' debug.log
```

## Getting Help

If issues persist:

1. Collect debug information:
   ```bash
   obsync version
   obsync config show
   obsync status
   ```

2. Create minimal reproduction case

3. Report issue with:
   - Obsync version
   - OS and version
   - Error messages
   - Debug logs (sanitize sensitive data)

4. Contact support or file issue on GitHub

## Common Error Codes

| Code | Meaning | Solution |
|------|---------|----------|
| AUTH_ERROR | Authentication failed | Re-login |
| VAULT_NOT_FOUND | Vault doesn't exist or no access | Check vault ID |
| DECRYPTION_ERROR | Failed to decrypt content | Verify password |
| INTEGRITY_ERROR | Hash mismatch | Retry sync |
| NETWORK_ERROR | Connection problem | Check network |
| STORAGE_ERROR | File system issue | Check permissions |
| RATE_LIMIT | Too many requests | Wait and retry |
| SERVER_ERROR | Server-side problem | Wait or contact support |