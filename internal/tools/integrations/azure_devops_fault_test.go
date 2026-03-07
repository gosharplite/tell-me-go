// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"testing/iotest"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
)

type mockFaultyTransport struct {
	resp *http.Response
	err  error
}

func (m *mockFaultyTransport) Do(req *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func TestHTTPClient_NetworkFault_MidStream(t *testing.T) {
	// 1. Define the specific hardware/network fault
	expectedErr := errors.New("simulated network drop mid-stream")

	// 2. Inject fault using standard library iotest.ErrReader
	mockClient := &mockFaultyTransport{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(iotest.ErrReader(expectedErr)),
		},
	}

	// 3. Initialize your client with the mock transport
	sm := security.NewSecurityManager(nil)
	m := newADOManager(sm, withHTTPClient(mockClient), withToken("test-pat"))

	// 4. Execute the call
	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "myorg",
		"project":      "myproj",
		"repository":   "myrepo",
		"path":         "/src/main.go",
	}
	_, err := m.adoGetFileContent(ctx, args)

	// 5. Assert the error bubbles up safely and retains its identity via %w
	if err == nil {
		t.Fatal("Expected I/O error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected wrapped error to match target fault. Got: %v", err)
	}

	// Verify the error message contains the expected context
	assert.Contains(t, err.Error(), "failed to read response body")
}

func TestHTTPClient_NetworkFault_OnStatusError(t *testing.T) {
	// 1. Define the specific hardware/network fault
	expectedErr := errors.New("simulated network drop on status error")

	// 2. Inject fault using standard library iotest.ErrReader on a non-200 response
	mockClient := &mockFaultyTransport{
		resp: &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(iotest.ErrReader(expectedErr)),
		},
	}

	// 3. Initialize your client with the mock transport
	sm := security.NewSecurityManager(nil)
	m := newADOManager(sm, withHTTPClient(mockClient), withToken("test-pat"))

	// 4. Execute the call
	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "myorg",
		"project":      "myproj",
		"repository":   "myrepo",
		"path":         "/src/main.go",
	}
	_, err := m.adoGetFileContent(ctx, args)

	// 5. Assert the error bubbles up safely and retains its identity via %w
	if err == nil {
		t.Fatal("Expected I/O error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected wrapped error to match target fault. Got: %v", err)
	}

	// Verify the error message contains the expected context from checkResponseError
	assert.Contains(t, err.Error(), "additionally, failed to read response body")
}
