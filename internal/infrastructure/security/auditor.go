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

// AuditLogger defines the interface for security logging.
type AuditLogger interface {
	LogAudit(label1, val1, label2, val2 string)
	SetLogFile(path string)
	SetInteractor(interactor domain.UserInteractor)
}

// Auditor handles security logging.
type Auditor struct {
	logFile    string
	mu         sync.Mutex
	interactor domain.UserInteractor
}

// NewAuditor creates a new Auditor.
func NewAuditor() *Auditor {
	return &Auditor{}
}

// SetInteractor sets the user interactor for warnings.
func (a *Auditor) SetInteractor(interactor domain.UserInteractor) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.interactor = interactor
}

// SetLogFile sets the path for logging executed commands.
func (a *Auditor) SetLogFile(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logFile = path
}

// LogAudit writes a two-line audit entry to the commands log file.
func (a *Auditor) LogAudit(label1, val1, label2, val2 string) {
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
	defer f.Close()

	fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, label1, val1)
	fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, label2, val2)
}

