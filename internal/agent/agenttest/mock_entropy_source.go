// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"
)

// MockEntropySource is a hand-rolled test double for io.Reader used as a
// source of entropy. Configure ReadFunc to control the behavior of Read;
// the function is responsible for copying data into p and returning the
// appropriate (n, err). The default (nil ReadFunc) returns (0, nil) — a
// valid zero-length read with no error.
//
// Snapshot() provides a concurrency-safe way to inspect call counts for
// test assertions.
type MockEntropySource struct {
	mu            sync.Mutex
	ReadFunc      func(p []byte) (n int, err error)
	readCalls     int
	calledMethods []string
}

// Read implements io.Reader. It delegates to ReadFunc if set, otherwise
// returns (0, nil). Call counts and method names are recorded under the
// mutex for later inspection via Snapshot.
func (m *MockEntropySource) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	m.readCalls++
	m.calledMethods = append(m.calledMethods, "Read")
	fn := m.ReadFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(p)
	}
	return 0, nil
}

// Snapshot returns a concurrency-safe snapshot of the accumulated Read
// call count and the ordered list of called method names. The returned
// slice is a defensive copy and safe to inspect without holding the lock.
func (m *MockEntropySource) Snapshot() (readCalls int, methods []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.readCalls, out
}
