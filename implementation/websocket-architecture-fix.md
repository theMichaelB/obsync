# WebSocket Reader Architecture Fix Implementation Plan

## Problem Statement

The current WebSocket implementation has a fundamental architectural issue where two different parts of the code attempt to read from the same WebSocket connection simultaneously:

1. **Main message processing loop** - Continuously reads messages via `StreamWS()` channel
2. **Pull request handler** - Attempts direct reads via `ReceiveJSONMessage()` and `ReceiveBinaryMessage()`

This creates a race condition where pull request responses are intercepted by the main loop, causing connection failures when the pull handler tries to read.

## Current Architecture Issues

### 1. Dual Reader Problem
```
WebSocket Connection
    ├── Main Loop Reader (via channel)
    └── Pull Request Reader (direct read) ← CONFLICT!
```

### 2. Message Flow Conflict
- Server sends pull response → Main loop reads it → Pull handler gets "connection closed"
- Both readers compete for the same data stream
- WebSocket connections only support one active reader

## Proposed Solution: Unified Message Router

### Architecture Overview

Implement a single WebSocket reader with a message routing system that handles both streaming messages and request/response patterns.

```
WebSocket Connection
    └── Unified Reader
         ├── Message Router
         │    ├── Push Messages → Process immediately
         │    ├── Ready Message → Trigger pull phase
         │    └── Pull Responses → Route to waiting handler
         └── Request Manager
              └── Pull Requests → Track pending requests
```

## Implementation Plan

### Phase 1: Create Message Router Infrastructure

#### 1.1 Add Request Tracking to Engine
```go
// Add to Engine struct
type Engine struct {
    // ... existing fields ...
    
    // Pull request tracking
    pullRequests   map[int]*PullRequestState  // uid -> request state
    pullMutex      sync.Mutex
    pullResponses  chan PullResponse
}

type PullRequestState struct {
    UID       int
    Path      string
    Hash      string
    Size      int
    Started   time.Time
    Metadata  *PullMetadata
    Data      []byte
    Complete  bool
    Error     error
}

type PullResponse struct {
    UID      int
    Type     string  // "metadata" or "binary"
    Metadata map[string]interface{}
    Data     []byte
    Error    error
}
```

#### 1.2 Modify Message Processing
```go
func (e *Engine) processMessage(ctx context.Context, msg models.WSMessage, ...) error {
    // Check if this is a pull response
    if e.isPullResponse(msg) {
        return e.routePullResponse(msg)
    }
    
    // Handle normal messages
    switch msg.Type {
    case "push":
        return e.handlePushMessage(ctx, msg, vaultKey, syncState)
    case "ready":
        // Don't exit loop - continue for pull responses
        return e.handleReadyMessage(ctx, msg, vaultKey, syncState)
    // ... other cases
    }
}
```

### Phase 2: Implement Pull Response Detection

#### 2.1 Add Response Detection Logic
```go
func (e *Engine) isPullResponse(msg models.WSMessage) bool {
    // Pull metadata responses have specific fields
    if msg.Type == "" || msg.Type == "pull_metadata" {
        data := make(map[string]interface{})
        if err := json.Unmarshal(msg.Data, &data); err == nil {
            if _, hasHash := data["hash"]; hasHash {
                if _, hasPieces := data["pieces"]; hasPieces {
                    return true
                }
            }
        }
    }
    
    // Binary responses are detected by message type
    return msg.Type == "binary" || msg.IsBinary
}
```

#### 2.2 Route Pull Responses
```go
func (e *Engine) routePullResponse(msg models.WSMessage) error {
    e.pullMutex.Lock()
    defer e.pullMutex.Unlock()
    
    // Determine response type and find matching request
    if msg.IsBinary {
        // Binary data - match to most recent metadata
        for uid, req := range e.pullRequests {
            if req.Metadata != nil && !req.Complete {
                e.pullResponses <- PullResponse{
                    UID:  uid,
                    Type: "binary",
                    Data: msg.BinaryData,
                }
                return nil
            }
        }
    } else {
        // JSON metadata - parse and match
        var metadata map[string]interface{}
        if err := json.Unmarshal(msg.Data, &metadata); err != nil {
            return fmt.Errorf("parse pull metadata: %w", err)
        }
        
        // Find request by matching characteristics
        if uid := e.findPullRequestByMetadata(metadata); uid > 0 {
            e.pullResponses <- PullResponse{
                UID:      uid,
                Type:     "metadata",
                Metadata: metadata,
            }
            return nil
        }
    }
    
    return fmt.Errorf("unmatched pull response")
}
```

### Phase 3: Redesign Pull Request Handler

#### 3.1 New Pull File Content Method
```go
func (e *Engine) pullFileContent(ctx context.Context, uid int, vaultKey []byte) ([]byte, error) {
    // Register pull request
    e.pullMutex.Lock()
    req := &PullRequestState{
        UID:     uid,
        Started: time.Now(),
    }
    e.pullRequests[uid] = req
    e.pullMutex.Unlock()
    
    // Send pull request
    pullMsg := map[string]interface{}{
        "op":  "pull",
        "uid": uid,
    }
    if err := e.transport.SendMessage(pullMsg); err != nil {
        return nil, fmt.Errorf("send pull request: %w", err)
    }
    
    // Wait for responses via channel
    return e.waitForPullResponse(ctx, uid, vaultKey)
}
```

#### 3.2 Response Waiting Logic
```go
func (e *Engine) waitForPullResponse(ctx context.Context, uid int, vaultKey []byte) ([]byte, error) {
    timeout := time.After(30 * time.Second)
    
    var metadata map[string]interface{}
    var binaryData []byte
    
    for {
        select {
        case resp := <-e.pullResponses:
            if resp.UID != uid {
                continue
            }
            
            switch resp.Type {
            case "metadata":
                metadata = resp.Metadata
                pieces := getInt(metadata, "pieces")
                if pieces == 0 {
                    // Empty file
                    return []byte{}, nil
                }
                // Continue waiting for binary data
                
            case "binary":
                if metadata == nil {
                    return nil, fmt.Errorf("binary data before metadata")
                }
                binaryData = resp.Data
                
                // Decrypt and return
                plainData, err := e.crypto.DecryptData(binaryData, vaultKey)
                if err != nil {
                    return nil, fmt.Errorf("decrypt: %w", err)
                }
                
                // Cleanup request
                e.pullMutex.Lock()
                delete(e.pullRequests, uid)
                e.pullMutex.Unlock()
                
                return plainData, nil
            }
            
        case <-timeout:
            return nil, fmt.Errorf("pull request timeout")
            
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }
}
```

### Phase 4: Modify WebSocket Reader

#### 4.1 Enhanced Message Reading
```go
// In websocket.go readLoop()
func (c *WSClient) readLoop() {
    defer close(c.messages)
    
    for {
        messageType, data, err := c.conn.ReadMessage()
        if err != nil {
            // ... error handling
        }
        
        // Create appropriate message based on type
        var msg models.WSMessage
        
        if messageType == websocket.BinaryMessage {
            msg = models.WSMessage{
                Type:       "binary",
                IsBinary:   true,
                BinaryData: data,
                Timestamp:  time.Now(),
            }
        } else {
            // Parse JSON for text messages
            var rawMsg map[string]interface{}
            if err := json.Unmarshal(data, &rawMsg); err != nil {
                continue
            }
            
            // Determine message type
            msgType := determineMessageType(rawMsg)
            
            msg = models.WSMessage{
                Type:      msgType,
                UID:       getInt(rawMsg, "uid"),
                Data:      json.RawMessage(data),
                Timestamp: time.Now(),
            }
        }
        
        c.messages <- msg
    }
}
```

### Phase 5: Handle State Transitions

#### 5.1 Modified Ready Handler
```go
func (e *Engine) handleReadyMessage(ctx context.Context, msg models.WSMessage, ...) error {
    // ... existing ready handling ...
    
    // Process pull queue but DON'T return ErrSyncComplete yet
    if len(e.pullQueue) > 0 {
        e.logger.Info("Starting pull request phase")
        
        // Set engine state to pull mode
        e.syncPhase = "pull"
        
        // Process queue - responses will come through main loop
        go e.processPullQueueAsync(ctx, vaultKey, syncState)
        
        // Continue reading messages for pull responses
        return nil
    }
    
    // Only complete if no pull requests needed
    return models.ErrSyncComplete
}
```

#### 5.2 Async Pull Queue Processing
```go
func (e *Engine) processPullQueueAsync(ctx context.Context, vaultKey []byte, syncState *models.SyncState) {
    for _, req := range e.pullQueue {
        if err := e.processSinglePullRequest(ctx, req, vaultKey, syncState); err != nil {
            e.logger.WithError(err).Error("Pull request failed")
        }
    }
    
    // Signal completion after all pulls done
    e.completePullPhase()
}
```

## Testing Strategy

### 1. Unit Tests
- Test message routing logic
- Test pull response detection
- Test request/response matching
- Test timeout handling

### 2. Integration Tests
- Mock WebSocket server with pull responses
- Test concurrent pull requests
- Test error scenarios
- Test large file handling

### 3. End-to-End Tests
- Full sync with real Obsidian server
- Verify all files downloaded correctly
- Test incremental sync after pull phase

## Migration Steps

1. **Backup current implementation** - Tag current version
2. **Implement message router** - Add new structures without breaking existing
3. **Add response detection** - Identify pull responses in main loop
4. **Migrate pull logic** - Switch to channel-based approach
5. **Remove old methods** - Clean up direct read methods
6. **Test thoroughly** - Run full test suite
7. **Deploy incrementally** - Test with small vaults first

## Risk Mitigation

1. **Backwards compatibility** - Maintain existing sync for non-pull files
2. **Timeout protection** - Ensure pulls don't hang indefinitely  
3. **Error recovery** - Graceful handling of partial downloads
4. **Memory management** - Clean up completed pull requests
5. **Connection stability** - Handle reconnects during pull phase

## Success Criteria

1. ✅ All files download successfully in full sync
2. ✅ No WebSocket connection errors during pulls
3. ✅ Pull requests complete within reasonable time
4. ✅ Memory usage remains stable during large syncs
5. ✅ Incremental syncs work after pull-based full sync

## Timeline Estimate

- **Phase 1**: Message Router - 2-3 hours
- **Phase 2**: Response Detection - 1-2 hours  
- **Phase 3**: Pull Handler Redesign - 2-3 hours
- **Phase 4**: WebSocket Modifications - 1-2 hours
- **Phase 5**: State Management - 1-2 hours
- **Testing**: 2-3 hours
- **Total**: 9-15 hours

This plan provides a systematic approach to fixing the WebSocket reader architecture while maintaining the integrity of the existing sync functionality.