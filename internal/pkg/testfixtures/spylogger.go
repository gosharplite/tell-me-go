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
//
// Use getErrors, getWarns, getInfos, getDebugs, or CalledWith to read
// recorded messages. Do not access the underlying slices directly since
// they are not protected by the mutex.
type SpyLogger struct {
	mu     sync.Mutex
	errors []string
	warns  []string
	infos  []string
	debugs []string
}

var _ ports.Logger = (*SpyLogger)(nil)

// GetErrors returns a snapshot copy of all Error-level messages recorded.
func (s *SpyLogger) GetErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.errors))
	copy(result, s.errors)
	return result
}

// GetWarns returns a snapshot copy of all Warn-level messages recorded.
func (s *SpyLogger) GetWarns() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.warns))
	copy(result, s.warns)
	return result
}

// getInfos returns a snapshot copy of all Info-level messages recorded.
func (s *SpyLogger) getInfos() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.infos))
	copy(result, s.infos)
	return result
}

// GetDebugs returns a snapshot copy of all Debug-level messages recorded.
func (s *SpyLogger) GetDebugs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.debugs))
	copy(result, s.debugs)
	return result
}

func (s *SpyLogger) Error(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, msg)
}

func (s *SpyLogger) Warn(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.warns = append(s.warns, msg)
}

func (s *SpyLogger) Info(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.infos = append(s.infos, msg)
}

func (s *SpyLogger) Debug(msg string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debugs = append(s.debugs, msg)
}

// CalledWith returns true if any call at the given level had the exact
// message. level must be one of "Error", "Warn", "Info", or "Debug".
func (s *SpyLogger) CalledWith(level, msg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	var slice []string
	switch level {
	case "Error":
		slice = s.errors
	case "Warn":
		slice = s.warns
	case "Info":
		slice = s.infos
	case "Debug":
		slice = s.debugs
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

// reset clears all recorded calls.
func (s *SpyLogger) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = nil
	s.warns = nil
	s.infos = nil
	s.debugs = nil
}
