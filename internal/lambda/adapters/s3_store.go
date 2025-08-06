package adapters

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/TheMichaelB/obsync/internal/events"
	"github.com/TheMichaelB/obsync/internal/storage"
)

type S3Store struct {
	client  *s3.Client
	bucket  string
	prefix  string
	logger  *events.Logger
}

func NewS3Store(bucket, prefix string, logger *events.Logger) (*S3Store, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	
	return &S3Store{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: prefix,
		logger: logger.WithField("component", "s3_store"),
	}, nil
}

// Implement storage.BlobStore interface

func (s *S3Store) Write(filePath string, data []byte, mode os.FileMode) error {
	key := s.buildKey(filePath)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
		Metadata: map[string]string{
			"mode": fmt.Sprintf("%o", mode),
			"path": filePath,
		},
	})
	
	if err != nil {
		return fmt.Errorf("s3 put object: %w", err)
	}
	
	s.logger.WithFields(map[string]interface{}{
		"key":  key,
		"size": len(data),
	}).Debug("Wrote file to S3")
	
	return nil
}

func (s *S3Store) WriteStream(filePath string, reader io.Reader, mode os.FileMode) error {
	// For Lambda, we need to buffer to memory first due to S3 SDK requirements
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	
	return s.Write(filePath, data, mode)
}

func (s *S3Store) Read(filePath string) ([]byte, error) {
	key := s.buildKey(filePath)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	
	if err != nil {
		return nil, fmt.Errorf("s3 get object: %w", err)
	}
	defer result.Body.Close()
	
	return io.ReadAll(result.Body)
}

func (s *S3Store) Delete(filePath string) error {
	key := s.buildKey(filePath)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	
	return err
}

func (s *S3Store) Exists(filePath string) (bool, error) {
	key := s.buildKey(filePath)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	
	if err != nil {
		// Check if it's a 404
		if strings.Contains(err.Error(), "NotFound") {
			return false, nil
		}
		return false, err
	}
	
	return true, nil
}

func (s *S3Store) Stat(filePath string) (storage.FileInfo, error) {
	key := s.buildKey(filePath)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	
	if err != nil {
		return storage.FileInfo{}, fmt.Errorf("s3 head object: %w", err)
	}
	
	return storage.FileInfo{
		Path:    filePath,
		Size:    aws.ToInt64(result.ContentLength),
		ModTime: aws.ToTime(result.LastModified),
		IsDir:   false,
	}, nil
}

func (s *S3Store) EnsureDir(dirPath string) error {
	// S3 doesn't need directories
	return nil
}

func (s *S3Store) ListDir(dirPath string) ([]storage.FileInfo, error) {
	prefix := s.buildKey(dirPath)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	var files []storage.FileInfo
	
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list objects: %w", err)
		}
		
		for _, obj := range page.Contents {
			// Convert S3 key back to file path
			relPath := strings.TrimPrefix(aws.ToString(obj.Key), prefix)
			files = append(files, storage.FileInfo{
				Path:    path.Join(dirPath, relPath),
				Size:    aws.ToInt64(obj.Size),
				ModTime: aws.ToTime(obj.LastModified),
				IsDir:   false,
			})
		}
	}
	
	return files, nil
}

func (s *S3Store) Move(oldPath, newPath string) error {
	// S3 doesn't have native move, so copy then delete
	data, err := s.Read(oldPath)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	
	if err := s.Write(newPath, data, 0644); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}
	
	return s.Delete(oldPath)
}

func (s *S3Store) SetModTime(filePath string, modTime time.Time) error {
	// S3 doesn't support modifying timestamps
	// This is tracked in metadata but not enforced
	return nil
}

// SetBasePath updates the S3 prefix for vault-specific storage
func (s *S3Store) SetBasePath(basePath string) error {
	// Extract vault name from the path for S3 prefix
	// basePath format: /tmp/vault-name or ./vault-name
	vaultName := filepath.Base(basePath)
	
	// Combine original prefix with vault name
	originalPrefix := strings.TrimSuffix(s.prefix, "/")
	if originalPrefix != "" {
		s.prefix = originalPrefix + "/" + vaultName + "/"
	} else {
		s.prefix = vaultName + "/"
	}
	
	s.logger.WithFields(map[string]interface{}{
		"base_path": basePath,
		"vault_name": vaultName,
		"s3_prefix": s.prefix,
	}).Debug("Updated S3 storage prefix for vault")
	
	return nil
}

func (s *S3Store) buildKey(filePath string) string {
	// Clean and normalize the path
	cleanPath := path.Clean(filePath)
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	
	if s.prefix != "" {
		return path.Join(s.prefix, cleanPath)
	}
	return cleanPath
}