// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"io"
	"net/http"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// MockHTTPClient is a test double for tools.HTTPClient.
//
// The zero value is usable — Do() returns (nil, nil) and records every
// request for later inspection via Snapshot().
//
// Set DoFunc to control return values; Snapshot() replaces testify-style
// AssertExpectations/AssertNumberOfCalls with race-safe introspection.
type MockHTTPClient struct {
	mu        sync.Mutex
	DoFunc    func(req *http.Request) (*http.Response, error)
	callCount int
	requests  []*http.Request
}

// Do implements tools.HTTPClient. It records the request, simulates
// body consumption like a real *http.Client, and delegates to DoFunc
// (or returns nil, nil when DoFunc is unset).
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.callCount++
	m.requests = append(m.requests, req)
	m.mu.Unlock()

	// Simulate real http.Client consuming the body after it's been sent.
	// This ensures retry tests that verify GetBody/body-reset work correctly.
	if req.Body != nil {
		_, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}

	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, nil
}

// Snapshot returns a race-safe copy of the observable call state.
// callCount is the number of times Do() was called; requests is a
// defensive copy of every request recorded.
func (m *MockHTTPClient) Snapshot() (callCount int, requests []*http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*http.Request, len(m.requests))
	copy(out, m.requests)
	return m.callCount, out
}

// Interface compliance check — ensures MockHTTPClient satisfies tools.HTTPClient.
var _ tools.HTTPClient = (*MockHTTPClient)(nil)
