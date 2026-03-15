// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// auditLogger defines the interface for security logging.
type auditLogger interface {
	LogAudit(action string, args ...any)
	SetLogFile(path string)
	SetInteractor(interactor domain.UserInteractor)
}

// auditor handles security logging.
type auditor struct {
	mu         sync.Mutex
	file       *os.File
	logger     *slog.Logger
	interactor domain.UserInteractor
}

// newAuditor creates a new auditor.
func newAuditor() *auditor {
	return &auditor{}
}

// SetInteractor sets the user interactor for warnings.
func (a *auditor) SetInteractor(interactor domain.UserInteractor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.interactor = interactor
}

// SetLogFile sets the path for logging executed commands.
func (a *auditor) SetLogFile(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.file != nil {
		_ = a.file.Close()
		a.file = nil
		a.logger = nil
	}

	if path == "" {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		if a.interactor != nil {
			a.interactor.Warn(fmt.Sprintf("[Warning] Failed to open command log file: %v", err))
		}
		return
	}

	a.file = f
	a.logger = slog.New(slog.NewTextHandler(f, nil))
}

// LogAudit writes an audit entry to the commands log file using slog.
func (a *auditor) LogAudit(action string, args ...any) {
	a.mu.Lock()
	logger := a.logger
	a.mu.Unlock()

	if logger != nil {
		logger.Info("AUDIT: "+action, args...)
	}
}
