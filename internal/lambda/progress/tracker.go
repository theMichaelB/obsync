package progress

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type ProgressTracker struct {
	dynamoClient *dynamodb.Client
	tableName    string
}

type SyncProgress struct {
	VaultID       string    `json:"vault_id"`
	StartTime     time.Time `json:"start_time"`
	LastUpdate    time.Time `json:"last_update"`
	FilesTotal    int       `json:"files_total"`
	FilesComplete int       `json:"files_complete"`
	BytesTotal    int64     `json:"bytes_total"`
	BytesComplete int64     `json:"bytes_complete"`
	Status        string    `json:"status"` // "running", "complete", "failed"
	Error         string    `json:"error,omitempty"`
	Resumable     bool      `json:"resumable"`
	LastVersion   int       `json:"last_version"`
	ResumeToken   string    `json:"resume_token,omitempty"`
}

func NewProgressTracker(dynamoClient *dynamodb.Client, tableName string) *ProgressTracker {
	return &ProgressTracker{
		dynamoClient: dynamoClient,
		tableName:    tableName,
	}
}

func (t *ProgressTracker) SaveProgress(ctx context.Context, progress SyncProgress) error {
	progress.LastUpdate = time.Now()
	
	progressJSON, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}
	
	_, err = t.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &t.tableName,
		Item: map[string]types.AttributeValue{
			"vault_id": &types.AttributeValueMemberS{
				Value: fmt.Sprintf("progress#%s", progress.VaultID),
			},
			"data": &types.AttributeValueMemberS{
				Value: string(progressJSON),
			},
			"ttl": &types.AttributeValueMemberN{
				Value: fmt.Sprintf("%d", time.Now().Add(24*time.Hour).Unix()),
			},
		},
	})
	
	return err
}

func (t *ProgressTracker) GetProgress(ctx context.Context, vaultID string) (*SyncProgress, error) {
	result, err := t.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &t.tableName,
		Key: map[string]types.AttributeValue{
			"vault_id": &types.AttributeValueMemberS{
				Value: fmt.Sprintf("progress#%s", vaultID),
			},
		},
	})
	
	if err != nil {
		return nil, err
	}
	
	if result.Item == nil {
		return nil, nil
	}
	
	dataAttr := result.Item["data"].(*types.AttributeValueMemberS)
	var progress SyncProgress
	if err := json.Unmarshal([]byte(dataAttr.Value), &progress); err != nil {
		return nil, err
	}
	
	return &progress, nil
}

func (t *ProgressTracker) DeleteProgress(ctx context.Context, vaultID string) error {
	_, err := t.dynamoClient.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &t.tableName,
		Key: map[string]types.AttributeValue{
			"vault_id": &types.AttributeValueMemberS{
				Value: fmt.Sprintf("progress#%s", vaultID),
			},
		},
	})
	
	return err
}