package events

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	
	"github.com/TheMichaelB/obsync/internal/config"
)

// LogLevel represents logging severity.
type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Logger provides structured logging.
type Logger struct {
	mu       sync.Mutex
	level    LogLevel
	format   string
	output   io.Writer
	fields   map[string]interface{}
	hostname string
}

// NewLogger creates a logger from config.
func NewLogger(cfg *config.LogConfig) (*Logger, error) {
	level := parseLevel(cfg.Level)
	
	var output io.Writer = os.Stdout
	if cfg.File != "" {
		// TODO: Add log rotation
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		output = file
	}
	
	hostname, _ := os.Hostname()
	
	return &Logger{
		level:    level,
		format:   cfg.Format,
		output:   output,
		fields:   make(map[string]interface{}),
		hostname: hostname,
	}, nil
}

// NewTestLogger creates a logger for testing.
func NewTestLogger(level LogLevel, format string, output io.Writer) *Logger {
	return &Logger{
		level:    level,
		format:   format,
		output:   output,
		fields:   make(map[string]interface{}),
		hostname: "test-host",
	}
}

// WithField returns a logger with an additional field.
func (l *Logger) WithField(key string, value interface{}) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	newFields := make(map[string]interface{}, len(l.fields)+1)
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value
	
	return &Logger{
		level:    l.level,
		format:   l.format,
		output:   l.output,
		fields:   newFields,
		hostname: l.hostname,
	}
}

// WithFields returns a logger with additional fields.
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	newFields := make(map[string]interface{}, len(l.fields)+len(fields))
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	
	return &Logger{
		level:    l.level,
		format:   l.format,
		output:   l.output,
		fields:   newFields,
		hostname: l.hostname,
	}
}

// WithError adds an error field.
func (l *Logger) WithError(err error) *Logger {
	return l.WithField("error", err.Error())
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string) {
	l.log(DebugLevel, msg)
}

// Info logs at info level.
func (l *Logger) Info(msg string) {
	l.log(InfoLevel, msg)
}

// Warn logs at warn level.
func (l *Logger) Warn(msg string) {
	l.log(WarnLevel, msg)
}

// Error logs at error level.
func (l *Logger) Error(msg string) {
	l.log(ErrorLevel, msg)
}

// log writes a log entry.
func (l *Logger) log(level LogLevel, msg string) {
	if level < l.level {
		return
	}
	
	l.mu.Lock()
	defer l.mu.Unlock()
	
	entry := l.buildEntry(level, msg)
	
	if l.format == "json" {
		l.writeJSON(entry)
	} else {
		l.writeText(entry)
	}
}

// buildEntry creates a log entry.
func (l *Logger) buildEntry(level LogLevel, msg string) map[string]interface{} {
	// Get caller info
	_, file, line, _ := runtime.Caller(3)
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		file = file[idx+1:]
	}
	
	entry := map[string]interface{}{
		"time":     time.Now().UTC().Format(time.RFC3339Nano),
		"level":    levelString(level),
		"msg":      msg,
		"hostname": l.hostname,
		"caller":   fmt.Sprintf("%s:%d", file, line),
	}
	
	// Add custom fields
	for k, v := range l.fields {
		entry[k] = v
	}
	
	return entry
}

// writeJSON outputs JSON format.
func (l *Logger) writeJSON(entry map[string]interface{}) {
	// Manual JSON construction for performance
	var sb strings.Builder
	sb.WriteString("{")
	
	first := true
	for k, v := range entry {
		if !first {
			sb.WriteString(",")
		}
		first = false
		
		sb.WriteString(fmt.Sprintf(`"%s":`, k))
		
		switch val := v.(type) {
		case string:
			sb.WriteString(fmt.Sprintf(`"%s"`, escapeJSON(val)))
		case int, int64, float64:
			sb.WriteString(fmt.Sprintf("%v", val))
		case bool:
			sb.WriteString(fmt.Sprintf("%v", val))
		default:
			sb.WriteString(fmt.Sprintf(`"%v"`, val))
		}
	}
	
	sb.WriteString("}\n")
	_, _ = l.output.Write([]byte(sb.String()))
}

// writeText outputs human-readable format.
func (l *Logger) writeText(entry map[string]interface{}) {
	levelStr := strings.ToUpper(entry["level"].(string))
	
	// Color codes for terminals
	var levelColor string
	if isTerminal(l.output) {
		switch levelStr {
		case "DEBUG":
			levelColor = "\033[36m" // Cyan
		case "INFO":
			levelColor = "\033[32m" // Green
		case "WARN":
			levelColor = "\033[33m" // Yellow
		case "ERROR":
			levelColor = "\033[31m" // Red
		}
	}
	
	// Format: TIME [LEVEL] Message key=value key=value
	fmt.Fprintf(l.output, "%s %s[%s]\033[0m %s",
		entry["time"],
		levelColor,
		levelStr,
		entry["msg"],
	)
	
	// Add fields
	for k, v := range entry {
		if k == "time" || k == "level" || k == "msg" || k == "hostname" || k == "caller" {
			continue
		}
		fmt.Fprintf(l.output, " %s=%v", k, v)
	}
	
	fmt.Fprintln(l.output)
}

// Helper functions

func parseLevel(s string) LogLevel {
	switch strings.ToLower(s) {
	case "debug":
		return DebugLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	default:
		return InfoLevel
	}
}

func levelString(l LogLevel) string {
	switch l {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	default:
		return "unknown"
	}
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isatty(f.Fd())
	}
	return false
}

// isatty is a simple terminal check
func isatty(fd uintptr) bool {
	return fd == 1 || fd == 2 // stdout or stderr
}