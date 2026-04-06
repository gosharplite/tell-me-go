// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type asyncTurnsLogger struct {
	file   infra_persistence.File
	ch     chan string
	wg     sync.WaitGroup
	logger *slog.Logger
	closed bool
	mu     sync.RWMutex
}

// NewAsyncTurnsLogger creates a new ports.TurnsLogger that writes to a file asynchronously.
func NewAsyncTurnsLogger(fs infra_persistence.FileSystem, filePath string, logger *slog.Logger) (ports.TurnsLogger, error) {
	f, err := fs.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open turns log file: %w", err)
	}

	tl := &asyncTurnsLogger{
		file:   f,
		ch:     make(chan string, 100),
		logger: logger,
	}

	tl.wg.Add(1)
	go tl.worker()

	return tl, nil
}

func (l *asyncTurnsLogger) worker() {
	defer l.wg.Done()
	for msg := range l.ch {
		if _, err := l.file.Write([]byte(msg)); err != nil {
			l.logger.Warn("failed to write to turns log", "error", err)
		} else if len(l.ch) == 0 {
			// Smart batching: only fsync when the channel buffer is fully drained
			_ = l.file.Sync()
		}
	}
}

func (l *asyncTurnsLogger) LogString(msg string) {
	if msg == "" {
		return
	}
	// Ensure string ends with newline
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return
	}

	select {
	case l.ch <- msg:
	default:
		l.logger.Warn("turns logger buffer full, dropping message")
	}
}

func (l *asyncTurnsLogger) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	close(l.ch)
	l.mu.Unlock()

	l.wg.Wait()
	return l.file.Close()
}
