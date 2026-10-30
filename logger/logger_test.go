package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewLogger(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.log")

	l, err := New(logPath, LevelInfo)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	l.Close()

	// File should be created.
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("log file not created: %v", err)
	}
}

func TestNewLoggerEmptyPath(t *testing.T) {
	l, err := New("", LevelInfo)
	if err != nil {
		t.Fatalf("New with empty path failed: %v", err)
	}
	l.Close()
}

func TestLoggerWritesToFile(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.log")

	l, err := New(logPath, LevelDebug)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer l.Close()

	l.Info("test message")
	l.Debug("debug message")
	l.Warn("warning message")
	l.Error("error message")

	// Flush and read.
	l.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "test message") {
		t.Error("INFO message not found in log")
	}
	if !strings.Contains(content, "debug message") {
		t.Error("DEBUG message not found in log")
	}
	if !strings.Contains(content, "WARNING") {
		t.Error("WARNING level not found in log")
	}
	if !strings.Contains(content, "ERROR") {
		t.Error("ERROR level not found in log")
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.log")

	l, err := New(logPath, LevelWarning)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	l.Debug("should not appear")
	l.Info("should not appear")
	l.Warn("should appear")
	l.Error("should appear")

	l.Close()

	data, _ := os.ReadFile(logPath)
	content := string(data)

	if strings.Contains(content, "should not appear") {
		t.Error("messages below level should not appear in log")
	}
	if !strings.Contains(content, "WARNING") {
		t.Error("WARNING messages should appear in log")
	}
	if !strings.Contains(content, "ERROR") {
		t.Error("ERROR messages should appear in log")
	}
}

func TestLoggerSetLevel(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.log")

	l, err := New(logPath, LevelError)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	l.Info("filtered out")
	l.Error("visible")

	l.SetLevel(LevelDebug)
	l.Debug("now visible")

	l.Close()

	data, _ := os.ReadFile(logPath)
	content := string(data)

	if strings.Contains(content, "filtered out") {
		t.Error("message should have been filtered at ERROR level")
	}
	if !strings.Contains(content, "visible") {
		t.Error("ERROR message should be visible")
	}
	if !strings.Contains(content, "now visible") {
		t.Error("DEBUG message should be visible after SetLevel")
	}
}

func TestLoggerSubscribe(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.log")

	l, err := New(logPath, LevelDebug)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer l.Close()

	ch := l.Subscribe()
	l.Info("subscriber test")

	select {
	case entry := <-ch:
		if entry.Message != "subscriber test" {
			t.Errorf("message: got %q, want %q", entry.Message, "subscriber test")
		}
		if entry.Level != LevelInfo {
			t.Errorf("level: got %v, want %v", entry.Level, LevelInfo)
		}
	default:
		t.Error("no entry received from subscription channel")
	}
}

func TestLoggerMultipleSubscribers(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "test.log")

	l, err := New(logPath, LevelDebug)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer l.Close()

	ch1 := l.Subscribe()
	ch2 := l.Subscribe()
	l.Info("broadcast")

	// Both subscribers should receive the message.
	select {
	case e := <-ch1:
		if e.Message != "broadcast" {
			t.Errorf("ch1 message: got %q", e.Message)
		}
	default:
		t.Error("ch1 did not receive message")
	}

	select {
	case e := <-ch2:
		if e.Message != "broadcast" {
			t.Errorf("ch2 message: got %q", e.Message)
		}
	default:
		t.Error("ch2 did not receive message")
	}
}

func TestAddFile(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "main.log")
	debugPath := filepath.Join(tmp, "debug.log")

	l, err := New(mainPath, LevelDebug)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	l.AddFile(debugPath)

	l.Info("test message")
	l.Close()

	// Both files should contain the message.
	mainData, _ := os.ReadFile(mainPath)
	debugData, _ := os.ReadFile(debugPath)

	if !strings.Contains(string(mainData), "test message") {
		t.Error("message not found in main log")
	}
	if !strings.Contains(string(debugData), "test message") {
		t.Error("message not found in debug log")
	}
}

// TestLoggerCloseClosesSubscribers verifies that Close signals subscriber
// channels, so that any goroutine range-ing over a subscription terminates
// instead of leaking.
func TestLoggerCloseClosesSubscribers(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")
	l, err := New(path, LevelDebug)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ch := l.Subscribe()
	if err := l.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// The subscription channel must now be closed.
	select {
	case entry, ok := <-ch:
		if ok {
			t.Errorf("expected a closed channel, got entry %+v", entry)
		}
	case <-time.After(time.Second):
		t.Error("subscription channel was not closed by Close")
	}

	// Subscribing after Close must return an already-closed channel.
	ch2 := l.Subscribe()
	select {
	case entry, ok := <-ch2:
		if ok {
			t.Errorf("expected a closed channel, got entry %+v", entry)
		}
	case <-time.After(time.Second):
		t.Error("post-Close subscription was not closed")
	}

	// Logging after Close must not panic or block.
	l.Info("post-close message")
	l.Close() // idempotent
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarning, "WARNING"},
		{LevelError, "ERROR"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.want {
			t.Errorf("Level(%d).String(): got %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestLoggerLevel(t *testing.T) {
	l, _ := New("", LevelWarning)
	defer l.Close()

	if l.Level() != LevelWarning {
		t.Errorf("Level(): got %v, want %v", l.Level(), LevelWarning)
	}
}
