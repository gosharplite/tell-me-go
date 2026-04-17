// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"io"
	"strings"
	"sync"
)

// MockInteractor is a test helper that implements UserInteractor.
type MockInteractor struct {
	mu     sync.Mutex
	Answer string
	Err    error
	Warns  []string
}

// Confirm returns the mocked answer.
func (m *MockInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	if m.Err != nil {
		return false, m.Err
	}
	ans := strings.ToLower(strings.TrimSpace(m.Answer))
	return ans == "y" || ans == "yes", nil
}

// Warn captures the warning message.
func (m *MockInteractor) Warn(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Warns = append(m.Warns, message)
}

// Prompt captures the prompt message as a warning.
func (m *MockInteractor) Prompt(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Warns = append(m.Warns, message)
}

// ReadSingleKey returns the first character of the mocked answer.
func (m *MockInteractor) ReadSingleKey(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if m.Err != nil {
		return "", m.Err
	}
	if m.Answer == "" {
		return "", nil
	}
	return strings.ToLower(m.Answer[:1]), nil
}

// ReadLine returns the mocked answer.
func (m *MockInteractor) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if m.Err != nil {
		return "", m.Err
	}
	if m.Answer == "" {
		return "", io.EOF
	}
	return m.Answer, nil
}
