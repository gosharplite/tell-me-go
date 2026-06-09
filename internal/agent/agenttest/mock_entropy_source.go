// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

// MockEntropySource is a hand-rolled test double for io.Reader used as a
// source of entropy. Configure ReadFunc to control the behavior of Read;
// the function is responsible for copying data into p and returning the
// appropriate (n, err). The default (nil ReadFunc) returns (0, nil) — a
// valid zero-length read with no error.
type MockEntropySource struct {
	ReadFunc func(p []byte) (n int, err error)
}

// Read implements io.Reader. It delegates to ReadFunc if set, otherwise
// returns (0, nil).
func (m *MockEntropySource) Read(p []byte) (n int, err error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(p)
	}
	return 0, nil
}
