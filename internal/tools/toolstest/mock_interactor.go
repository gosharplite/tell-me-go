// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"io"
)

// MockInteractor is a test double for security.UserInteractor used by
// internal/tools test suites. Confirm returns true when Answer is "y"
// or "yes"; ReadSingleKey returns Answer verbatim; ReadLine returns
// Answer or io.EOF when Answer is empty. Warn and Prompt append their
// messages to the Warns and Prompts slices respectively. If Err is set
// it short-circuits Confirm/ReadSingleKey/ReadLine.
//
// This mock is intentionally kept distinct from the package-private
// MockInteractor in internal/infrastructure/security: that one is
// optimized for the security package's own tests (sync.Mutex,
// ctx-cancellation, lower-cased single-key answers); this one matches
// the looser semantics that internal/tools tests rely on (Prompts
// slice, no mutex, raw Answer for ReadSingleKey).
type MockInteractor struct {
	Answer  string
	Warns   []string
	Prompts []string
	Err     error
}

func (m *MockInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	return m.Answer == "y" || m.Answer == "yes", nil
}

func (m *MockInteractor) Warn(message string) {
	m.Warns = append(m.Warns, message)
}

func (m *MockInteractor) Prompt(message string) {
	m.Prompts = append(m.Prompts, message)
}

func (m *MockInteractor) ReadSingleKey(ctx context.Context) (string, error) {
	return m.Answer, m.Err
}

func (m *MockInteractor) ReadLine(ctx context.Context) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	if m.Answer == "" {
		return "", io.EOF
	}
	return m.Answer, nil
}
