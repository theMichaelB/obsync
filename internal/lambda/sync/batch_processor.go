package sync

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type BatchProcessor struct {
	batchSize     int
	maxConcurrent int
	memManager    *MemoryManager
}

func NewBatchProcessor(memManager *MemoryManager) *BatchProcessor {
	return &BatchProcessor{
		batchSize:     100,  // Files per batch
		maxConcurrent: 5,    // Concurrent batches
		memManager:    memManager,
	}
}

type FileInfo struct {
	Path string
	Hash string
	Size int64
}

func (p *BatchProcessor) ProcessFiles(ctx context.Context, files []FileInfo, 
	processFunc func(context.Context, []FileInfo) error) error {
	
	// Split into batches
	batches := p.splitIntoBatches(files)
	
	// Process batches with concurrency control
	sem := make(chan struct{}, p.maxConcurrent)
	errChan := make(chan error, len(batches))
	var wg sync.WaitGroup
	
	for i, batch := range batches {
		// Check if we should pause for memory
		for p.memManager.ShouldPause() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
				continue
			}
		}
		
		wg.Add(1)
		sem <- struct{}{}
		
		go func(batchNum int, files []FileInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			
			if err := processFunc(ctx, files); err != nil {
				errChan <- fmt.Errorf("batch %d: %w", batchNum, err)
			}
		}(i, batch)
	}
	
	wg.Wait()
	close(errChan)
	
	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("batch processing failed with %d errors: %v", len(errs), errs[0])
	}
	
	return nil
}

func (p *BatchProcessor) splitIntoBatches(files []FileInfo) [][]FileInfo {
	var batches [][]FileInfo
	
	for i := 0; i < len(files); i += p.batchSize {
		end := i + p.batchSize
		if end > len(files) {
			end = len(files)
		}
		batches = append(batches, files[i:end])
	}
	
	return batches
}