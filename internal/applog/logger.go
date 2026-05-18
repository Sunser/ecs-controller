package applog

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

type Entry struct {
	Time    time.Time         `json:"time"`
	Level   Level             `json:"level"`
	Module  string            `json:"module"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type Logger struct {
	mu       sync.Mutex
	capacity int
	out      io.Writer
	minLevel Level
	entries  []Entry
	next     int
	full     bool
}

var defaultLogger = New(300, os.Stderr)

func New(capacity int, out io.Writer) *Logger {
	if capacity <= 0 {
		capacity = 300
	}
	if out == nil {
		out = io.Discard
	}
	return &Logger{
		capacity: capacity,
		out:      out,
		minLevel: LevelInfo,
		entries:  make([]Entry, capacity),
	}
}

func SetDefault(logger *Logger) {
	if logger == nil {
		return
	}
	defaultLogger = logger
}

func Default() *Logger {
	return defaultLogger
}

func Info(module, message string, fields map[string]string) {
	defaultLogger.Write(LevelInfo, module, message, fields)
}

func Debug(module, message string, fields map[string]string) {
	defaultLogger.Write(LevelDebug, module, message, fields)
}

func Warn(module, message string, fields map[string]string) {
	defaultLogger.Write(LevelWarn, module, message, fields)
}

func Error(module, message string, fields map[string]string) {
	defaultLogger.Write(LevelError, module, message, fields)
}

func Snapshot(limit int) []Entry {
	return defaultLogger.Snapshot(limit)
}

func SetLevel(level string) {
	defaultLogger.SetLevel(level)
}

func (l *Logger) Write(level Level, module, message string, fields map[string]string) {
	l.writeAt(time.Now(), level, module, message, fields)
}

func (l *Logger) SetLevel(level string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = parseLevel(level)
}

func (l *Logger) Snapshot(limit int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := l.next
	if l.full {
		count = l.capacity
	}
	if limit <= 0 || limit > count {
		limit = count
	}
	start := count - limit
	result := make([]Entry, 0, limit)
	for i := start; i < count; i++ {
		index := i
		if l.full {
			index = (l.next + i) % l.capacity
		}
		entry := l.entries[index]
		entry.Fields = cloneFields(entry.Fields)
		result = append(result, entry)
	}
	return result
}

func (l *Logger) writeAt(at time.Time, level Level, module, message string, fields map[string]string) {
	l.mu.Lock()
	if levelRank(level) < levelRank(l.minLevel) {
		l.mu.Unlock()
		return
	}
	entry := Entry{
		Time:    at,
		Level:   level,
		Module:  module,
		Message: message,
		Fields:  cloneFields(fields),
	}
	line := formatEntry(entry)

	l.entries[l.next] = entry
	l.next = (l.next + 1) % l.capacity
	if l.next == 0 {
		l.full = true
	}
	_, _ = l.out.Write([]byte(line + "\n"))
	l.mu.Unlock()
}

func parseLevel(level string) Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func levelRank(level Level) int {
	switch level {
	case LevelDebug:
		return 10
	case LevelInfo:
		return 20
	case LevelWarn:
		return 30
	case LevelError:
		return 40
	default:
		return 20
	}
}

func formatEntry(entry Entry) string {
	parts := []string{
		entry.Time.Format("2006-01-02 15:04:05"),
		"[" + string(entry.Level) + "]",
		entry.Module,
		entry.Message,
	}
	keys := make([]string, 0, len(entry.Fields))
	for key := range entry.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := sanitizeField(entry.Fields[key])
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(parts, " ")
}

func sanitizeField(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func cloneFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	copyFields := make(map[string]string, len(fields))
	for key, value := range fields {
		copyFields[key] = value
	}
	return copyFields
}
