package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu  sync.Mutex
	w   io.Writer
	cls io.Closer
}

func New(path string) (*Logger, error) {
	if path == "" {
		return &Logger{w: os.Stderr}, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{w: f, cls: f}, nil
}

func (l *Logger) Close() error {
	if l.cls != nil {
		return l.cls.Close()
	}
	return nil
}

func (l *Logger) Logf(level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(l.w, "%s [%s] %s\n", ts, level, fmt.Sprintf(format, args...))
}

func (l *Logger) Info(format string, args ...interface{})  { l.Logf("INFO", format, args...) }
func (l *Logger) Warn(format string, args ...interface{})  { l.Logf("WARN", format, args...) }
func (l *Logger) Error(format string, args ...interface{}) { l.Logf("ERROR", format, args...) }
func (l *Logger) Debug(format string, args ...interface{}) { l.Logf("DEBUG", format, args...) }
