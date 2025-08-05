package sync

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
	
	"github.com/TheMichaelB/obsync/internal/events"
)

type MemoryManager struct {
	logger        *events.Logger
	maxMemoryMB   int64
	maxTmpSizeMB  int64
	checkInterval time.Duration
	mu            sync.RWMutex
	paused        bool
}

func NewMemoryManager(logger *events.Logger) *MemoryManager {
	return &MemoryManager{
		logger:        logger,
		maxMemoryMB:   getMaxMemoryMB(),
		maxTmpSizeMB:  400, // Leave 100MB buffer in /tmp
		checkInterval: 5 * time.Second,
	}
}

func (m *MemoryManager) Start() {
	go m.monitor()
}

func (m *MemoryManager) monitor() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		m.checkResources()
	}
}

func (m *MemoryManager) checkResources() {
	// Check memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	usedMB := int64(memStats.Alloc / 1024 / 1024)
	percentUsed := float64(usedMB) / float64(m.maxMemoryMB) * 100
	
	// Check /tmp usage
	tmpUsedMB := m.getTmpUsage()
	tmpPercentUsed := float64(tmpUsedMB) / float64(m.maxTmpSizeMB) * 100
	
	m.logger.WithFields(map[string]interface{}{
		"memory_used_mb":     usedMB,
		"memory_percent":     percentUsed,
		"tmp_used_mb":        tmpUsedMB,
		"tmp_percent":        tmpPercentUsed,
	}).Debug("Resource usage")
	
	// Pause sync if resources are low
	m.mu.Lock()
	if percentUsed > 80 || tmpPercentUsed > 80 {
		if !m.paused {
			m.paused = true
			m.logger.Warn("Pausing sync due to resource constraints")
			runtime.GC() // Force garbage collection
		}
	} else if m.paused && percentUsed < 60 && tmpPercentUsed < 60 {
		m.paused = false
		m.logger.Info("Resuming sync after resource recovery")
	}
	m.mu.Unlock()
}

func (m *MemoryManager) ShouldPause() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused
}

func (m *MemoryManager) getTmpUsage() int64 {
	var stat os.FileInfo
	var err error
	
	stat, err = os.Stat("/tmp")
	if err != nil {
		return 0
	}
	
	// Note: This is simplified - in production, use syscall.Statfs
	return stat.Size() / 1024 / 1024
}

func getMaxMemoryMB() int64 {
	// Get from Lambda environment
	memSize := os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE")
	if memSize == "" {
		return 1024 // Default 1GB
	}
	
	var size int64
	fmt.Sscanf(memSize, "%d", &size)
	return size
}