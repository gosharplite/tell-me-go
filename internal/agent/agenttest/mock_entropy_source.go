// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"github.com/stretchr/testify/mock"
)

// MockEntropySource is a testify-based test double for io.Reader used
// as a source of entropy. Configure with mock.On("Read", ...).Return(
// []byte{...}, n, err) — the bytes are copied into the caller's
// buffer, and n/err are returned verbatim.
type MockEntropySource struct {
	mock.Mock
}

func (m *MockEntropySource) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	if args.Get(0) != nil {
		copy(p, args.Get(0).([]byte))
	}
	return args.Int(1), args.Error(2)
}
