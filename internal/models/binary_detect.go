package models

import (
	"bytes"
	"path/filepath"
	"strings"
)

// Common binary file extensions
var binaryExtensions = map[string]bool{
	".png":  true, ".jpg":  true, ".jpeg": true, ".gif":  true, ".bmp":  true,
	".ico":  true, ".tiff": true, ".webp": true, ".svg":  true,
	".pdf":  true, ".doc":  true, ".docx": true, ".xls":  true, ".xlsx": true,
	".ppt":  true, ".pptx": true, ".odt":  true, ".ods":  true, ".odp":  true,
	".zip":  true, ".rar":  true, ".7z":   true, ".tar":  true, ".gz":   true,
	".bz2":  true, ".xz":   true,
	".exe":  true, ".dll":  true, ".so":   true, ".dylib": true,
	".mp3":  true, ".mp4":  true, ".avi":  true, ".mkv":  true, ".mov":  true,
	".wav":  true, ".flac": true, ".aac":  true, ".ogg":  true, ".wma":  true,
	".ttf":  true, ".otf":  true, ".woff": true, ".woff2": true, ".eot":  true,
}

// IsBinaryFile detects if a file is binary based on extension or content.
func IsBinaryFile(path string, content []byte) bool {
	// Check extension first
	ext := strings.ToLower(filepath.Ext(path))
	if binaryExtensions[ext] {
		return true
	}
	
	// For unknown extensions, check content
	if len(content) == 0 {
		return false
	}
	
	// Check for null bytes in first 8KB
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}
	
	if bytes.IndexByte(content[:checkLen], 0) != -1 {
		return true
	}
	
	// Check for high proportion of non-printable characters
	nonPrintable := 0
	for i := 0; i < checkLen; i++ {
		b := content[i]
		if b < 32 && b != '\t' && b != '\n' && b != '\r' {
			nonPrintable++
		}
	}
	
	// If more than 30% non-printable, consider binary
	return float64(nonPrintable)/float64(checkLen) > 0.3
}