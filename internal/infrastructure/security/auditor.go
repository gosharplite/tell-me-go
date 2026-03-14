// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"fmt"
	"os"
	"sync"
	"time"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// auditLogger defines the interface for security logging.
type auditLogger interface {
	LogAudit(action string, fields map[string]any)
	SetLogFile(path string)
	SetInteractor(interactor domain.UserInteractor)
}

// auditor handles security logging.
type auditor struct {
	logFile    string
	mu         sync.Mutex
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
	a.logFile = path
}

// LogAudit writes a multi-line audit entry to the commands log file.
func (a *auditor) LogAudit(action string, fields map[string]any) {
	a.mu.Lock()
	interactor := a.interactor
	logFile := a.logFile
	a.mu.Unlock()

	if logFile == "" {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		if interactor != nil {
			interactor.Warn(fmt.Sprintf("[Warning] Failed to open command log file: %v", err))
		}
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && interactor != nil {
			interactor.Warn(fmt.Sprintf("[Warning] Failed to close command log file: %v", cerr))
		}
	}()

	fmt.Fprintf(f, "[%s] ACTION: %s\n", timestamp, action)
	for label, val := range fields {
		fmt.Fprintf(f, "[%s] %s: %v\n", timestamp, label, val)
	}
}
