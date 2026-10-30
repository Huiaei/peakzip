// Package logger provides leveled logging with file output and UI subscriber support.
package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents the severity of a log message.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarning
	LevelError
)

// String returns the string representation of the level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarning:
		return "WARNING"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Entry represents a single log record.
type Entry struct {
	Time    time.Time
	Level   Level
	Message string
}

// Logger handles leveled logging to file and UI subscribers.
type Logger struct {
	mu     sync.Mutex
	level  Level
	files  []*os.File
	subs   []chan Entry
	closed bool
}

// New creates a new Logger with the given log file path and minimum level.
// If path is empty, no file output is written.
func New(path string, level Level) (*Logger, error) {
	l := &Logger{
		level: level,
	}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		l.files = append(l.files, f)
	}
	return l, nil
}

// AddFile adds an additional output file. Used for a second log target
// (e.g. a debug log in the working directory).
func (l *Logger) AddFile(path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.files = append(l.files, f)
	l.mu.Unlock()
}

// Close closes the underlying log files and signals every subscriber
// channel so that any goroutine range-ing over a subscription terminates.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	var firstErr error
	for _, f := range l.files {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	l.files = nil
	for _, ch := range l.subs {
		close(ch)
	}
	l.subs = nil
	l.closed = true
	return firstErr
}

// Subscribe returns a channel that receives log entries at or above the
// logger's minimum level. Channels are buffered; if full, new entries are dropped.
//
// If the logger has already been Closed, a closed channel is returned so that
// any range-loop over it terminates immediately.
func (l *Logger) Subscribe() <-chan Entry {
	ch := make(chan Entry, 256)
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		close(ch)
		return ch
	}
	l.subs = append(l.subs, ch)
	l.mu.Unlock()
	return ch
}

// Log writes a message at the given level.
func (l *Logger) Log(level Level, msg string) {
	if level < l.level {
		return
	}
	entry := Entry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, f := range l.files {
		fmt.Fprintf(f, "[%s] [%s] %s\n",
			entry.Time.Format(time.RFC3339), level.String(), msg)
	}
	for _, ch := range l.subs {
		select {
		case ch <- entry:
		default: // drop if full
		}
	}
}

// Debug logs at DEBUG level.
func (l *Logger) Debug(msg string) { l.Log(LevelDebug, msg) }

// Info logs at INFO level.
func (l *Logger) Info(msg string) { l.Log(LevelInfo, msg) }

// Warn logs at WARNING level.
func (l *Logger) Warn(msg string) { l.Log(LevelWarning, msg) }

// Error logs at ERROR level.
func (l *Logger) Error(msg string) { l.Log(LevelError, msg) }

// SetLevel changes the minimum log level at runtime.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Level returns the current minimum log level.
func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

// ioWriter wraps *os.File so it satisfies io.Writer.
var _ io.Writer = (*os.File)(nil)
