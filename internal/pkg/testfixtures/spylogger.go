// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testfixtures

import (
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// SpyLogger implements ports.Logger and records every call for test
// assertion. All four log levels are captured as strings (the msg
// argument only; key-value args are not stored). It is goroutine-safe.
//
// Zero value is ready to use.
type SpyLogger struct {
	mu     sync.Mutex
	Errors []string
	Warns  []string
	Infos  []string
	Debugs []string
}

var _ ports.Logger = (*SpyLogger)(nil)

func (s *SpyLogger) Error(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Errors = append(s.Errors, msg)
}

func (s *SpyLogger) Warn(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Warns = append(s.Warns, msg)
}

func (s *SpyLogger) Info(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Infos = append(s.Infos, msg)
}

func (s *SpyLogger) Debug(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Debugs = append(s.Debugs, msg)
}

// CalledWith returns true if any call at the given level had the exact
// message. level must be one of "Error", "Warn", "Info", or "Debug".
func (s *SpyLogger) CalledWith(level, msg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	var slice []string
	switch level {
	case "Error":
		slice = s.Errors
	case "Warn":
		slice = s.Warns
	case "Info":
		slice = s.Infos
	case "Debug":
		slice = s.Debugs
	default:
		return false
	}
	for _, m := range slice {
		if m == msg {
			return true
		}
	}
	return false
}

// Reset clears all recorded calls.
func (s *SpyLogger) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Errors = nil
	s.Warns = nil
	s.Infos = nil
	s.Debugs = nil
}
