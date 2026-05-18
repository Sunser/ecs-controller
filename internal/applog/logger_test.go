package applog

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesLevelAndFieldsWithoutTimezoneSuffix(t *testing.T) {
	var out bytes.Buffer
	logger := New(10, &out)
	now := time.Date(2026, 5, 18, 20, 45, 6, 0, time.Local)

	logger.writeAt(now, LevelInfo, "server", "listening", map[string]string{"addr": ":8080"})

	line := out.String()
	if !strings.Contains(line, "2026-05-18 20:45:06 [INFO] server listening addr=:8080") {
		t.Fatalf("log line = %q", line)
	}
	if strings.Contains(line, "CST") || strings.Contains(line, "Asia/Shanghai") || strings.Contains(line, "上海") {
		t.Fatalf("log line should not include timezone suffix: %q", line)
	}
}

func TestLoggerSnapshotKeepsRecentEntriesInOrder(t *testing.T) {
	logger := New(2, &bytes.Buffer{})
	base := time.Date(2026, 5, 18, 20, 0, 0, 0, time.Local)

	logger.writeAt(base, LevelInfo, "refresh", "first", nil)
	logger.writeAt(base.Add(time.Second), LevelWarn, "refresh", "second", nil)
	logger.writeAt(base.Add(2*time.Second), LevelError, "refresh", "third", nil)

	entries := logger.Snapshot(10)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Message != "second" || entries[1].Message != "third" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestLoggerFiltersBelowConfiguredLevel(t *testing.T) {
	var out bytes.Buffer
	logger := New(10, &out)
	logger.SetLevel("warn")
	now := time.Date(2026, 5, 18, 20, 45, 6, 0, time.Local)

	logger.writeAt(now, LevelDebug, "traffic", "debug", nil)
	logger.writeAt(now, LevelInfo, "traffic", "info", nil)
	logger.writeAt(now, LevelWarn, "traffic", "warn", nil)

	line := out.String()
	if strings.Contains(line, "debug") || strings.Contains(line, "info") {
		t.Fatalf("filtered log leaked: %q", line)
	}
	if !strings.Contains(line, "[WARN] traffic warn") {
		t.Fatalf("warn log missing: %q", line)
	}
}
