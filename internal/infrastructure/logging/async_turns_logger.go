// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type asyncTurnsLogger struct {
	file   *os.File
	ch     chan string
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// NewAsyncTurnsLogger creates a new ports.TurnsLogger that writes to a file asynchronously.
func NewAsyncTurnsLogger(filePath string) (ports.TurnsLogger, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open turns log file: %w", err)
	}

	l := &asyncTurnsLogger{
		file: f,
		ch:   make(chan string, 100),
	}

	l.wg.Add(1)
	go l.processLogs()

	return l, nil
}

func (l *asyncTurnsLogger) processLogs() {
	defer l.wg.Done()
	for msg := range l.ch {
		_, _ = l.file.WriteString(msg)
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

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	select {
	case l.ch <- msg:
	default:
		// Buffer full, drop message
	}
	l.mu.Unlock()
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
