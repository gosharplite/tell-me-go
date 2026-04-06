// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type asyncTurnsLogger struct {
	file   infra_persistence.File
	ch     chan string
	wg     sync.WaitGroup
	logger *slog.Logger
	closed atomic.Bool
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

	if l.closed.Load() {
		return
	}

	// Safely handle the edge case where the channel is closed exactly between
	// the atomic check and the channel send.
	defer func() {
		recover()
	}()

	select {
	case l.ch <- msg:
	default:
		l.logger.Warn("turns logger buffer full, dropping message")
	}
}

func (l *asyncTurnsLogger) Close() error {
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(l.ch)
	l.wg.Wait()
	return l.file.Close()
}
