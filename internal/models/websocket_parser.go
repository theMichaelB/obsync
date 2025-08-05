package models

import (
	"encoding/json"
	"fmt"
)

// ParseWSMessage parses a raw WebSocket message.
func ParseWSMessage(data []byte) (*WSMessage, error) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("parse ws message: %w", err)
	}
	return &msg, nil
}

// ParseMessageData parses the data field based on message type.
func ParseMessageData(msg *WSMessage) (interface{}, error) {
	switch msg.Type {
	case WSTypeInitResponse:
		var data InitResponse
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return nil, fmt.Errorf("parse init response: %w", err)
		}
		return &data, nil
		
	case WSTypeFile:
		var data FileMessage
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return nil, fmt.Errorf("parse file message: %w", err)
		}
		return &data, nil
		
	case WSTypeFolder:
		var data FolderMessage
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return nil, fmt.Errorf("parse folder message: %w", err)
		}
		return &data, nil
		
	case WSTypeDelete:
		var data DeleteMessage
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return nil, fmt.Errorf("parse delete message: %w", err)
		}
		return &data, nil
		
	case WSTypeDone:
		var data DoneMessage
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return nil, fmt.Errorf("parse done message: %w", err)
		}
		return &data, nil
		
	case WSTypeError:
		var data ErrorMessage
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return nil, fmt.Errorf("parse error message: %w", err)
		}
		return &data, nil
		
	default:
		return nil, fmt.Errorf("unknown message type: %s", msg.Type)
	}
}