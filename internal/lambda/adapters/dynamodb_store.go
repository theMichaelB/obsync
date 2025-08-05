package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
	
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/models"
	"github.com/TheMichaelB/obsync/internal/state"
)

type DynamoDBStore struct {
	client    *dynamodb.Client
	tableName string
	logger    *events.Logger
}

func NewDynamoDBStore(tableName string, logger *events.Logger) (*DynamoDBStore, error) {
	if tableName == "" {
		tableName = os.Getenv("STATE_TABLE_NAME")
		if tableName == "" {
			return nil, fmt.Errorf("STATE_TABLE_NAME environment variable required")
		}
	}
	
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	
	return &DynamoDBStore{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
		logger:    logger.WithField("component", "dynamodb_store"),
	}, nil
}

// Implement state.Store interface

func (s *DynamoDBStore) Load(vaultID string) (*models.SyncState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"vault_id": &types.AttributeValueMemberS{Value: vaultID},
		},
	})
	
	if err != nil {
		return nil, fmt.Errorf("dynamodb get: %w", err)
	}
	
	if result.Item == nil {
		// No state found, return new state
		s.logger.WithField("vault_id", vaultID).Debug("No state found, creating new")
		return models.NewSyncState(vaultID), nil
	}
	
	// Parse state JSON from DynamoDB
	stateAttr, ok := result.Item["state"].(*types.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("invalid state attribute type")
	}
	
	var syncState models.SyncState
	if err := json.Unmarshal([]byte(stateAttr.Value), &syncState); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	
	s.logger.WithField("vault_id", vaultID).Debug("Loaded state from DynamoDB")
	return &syncState, nil
}

func (s *DynamoDBStore) Save(vaultID string, syncState *models.SyncState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	stateJSON, err := json.Marshal(syncState)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	
	item := map[string]types.AttributeValue{
		"vault_id": &types.AttributeValueMemberS{Value: vaultID},
		"state": &types.AttributeValueMemberS{Value: string(stateJSON)},
		"updated_at": &types.AttributeValueMemberN{
			Value: fmt.Sprintf("%d", time.Now().Unix()),
		},
		"version": &types.AttributeValueMemberN{
			Value: fmt.Sprintf("%d", syncState.Version),
		},
	}
	
	// Add TTL if table has TTL enabled (30 days)
	ttl := time.Now().Add(30 * 24 * time.Hour).Unix()
	item["ttl"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", ttl)}
	
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	
	if err != nil {
		return fmt.Errorf("dynamodb put: %w", err)
	}
	
	s.logger.WithFields(map[string]interface{}{
		"vault_id": vaultID,
		"version":  syncState.Version,
	}).Debug("Saved state to DynamoDB")
	
	return nil
}

func (s *DynamoDBStore) Reset(vaultID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"vault_id": &types.AttributeValueMemberS{Value: vaultID},
		},
	})
	
	if err != nil {
		return fmt.Errorf("dynamodb delete: %w", err)
	}
	
	s.logger.WithField("vault_id", vaultID).Info("Reset state in DynamoDB")
	return nil
}

func (s *DynamoDBStore) List() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	var vaultIDs []string
	
	paginator := dynamodb.NewScanPaginator(s.client, &dynamodb.ScanInput{
		TableName:            aws.String(s.tableName),
		ProjectionExpression: aws.String("vault_id"),
	})
	
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("dynamodb scan: %w", err)
		}
		
		for _, item := range page.Items {
			if vaultAttr, ok := item["vault_id"].(*types.AttributeValueMemberS); ok {
				vaultIDs = append(vaultIDs, vaultAttr.Value)
			}
		}
	}
	
	return vaultIDs, nil
}

func (s *DynamoDBStore) Lock(vaultID string) (state.UnlockFunc, error) {
	// For Lambda, we rely on DynamoDB conditional writes instead of explicit locks
	// since Lambda functions are stateless
	s.logger.WithField("vault_id", vaultID).Debug("Lock requested (no-op in Lambda)")
	
	return func() {
		s.logger.WithField("vault_id", vaultID).Debug("Unlock called (no-op in Lambda)")
	}, nil
}

func (s *DynamoDBStore) Migrate(target state.Store) error {
	vaultIDs, err := s.List()
	if err != nil {
		return fmt.Errorf("list vaults: %w", err)
	}
	
	for _, vaultID := range vaultIDs {
		state, err := s.Load(vaultID)
		if err != nil {
			return fmt.Errorf("load vault %s: %w", vaultID, err)
		}
		
		if err := target.Save(vaultID, state); err != nil {
			return fmt.Errorf("save vault %s: %w", vaultID, err)
		}
	}
	
	return nil
}

func (s *DynamoDBStore) Close() error {
	// No resources to clean up
	return nil
}