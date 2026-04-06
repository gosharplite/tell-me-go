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

type syncTurnsLogger struct {
	file   *os.File
	closed bool
	mu     sync.Mutex
}

// NewSyncTurnsLogger creates a new ports.TurnsLogger that writes to a file synchronously.
func NewSyncTurnsLogger(filePath string) (ports.TurnsLogger, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open turns log file: %w", err)
	}

	return &syncTurnsLogger{
		file: f,
	}, nil
}

func (l *syncTurnsLogger) LogString(msg string) {
	if msg == "" {
		return
	}
	// Ensure string ends with newline
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	
	if !l.closed {
		_, _ = l.file.WriteString(msg)
	}
}

func (l *syncTurnsLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	
	if l.closed {
		return nil
	}
	
	l.closed = true
	return l.file.Close()
}
