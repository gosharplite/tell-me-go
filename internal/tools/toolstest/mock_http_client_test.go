// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestMockHTTPClient_ZeroValue(t *testing.T) {
	m := &toolstest.MockHTTPClient{}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)

	resp, err := m.Do(req)
	if resp != nil {
		t.Errorf("expected nil response from zero-value mock, got %v", resp)
	}
	if err != nil {
		t.Errorf("expected nil error from zero-value mock, got %v", err)
	}
}

func TestMockHTTPClient_DoFunc(t *testing.T) {
	m := &toolstest.MockHTTPClient{}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)

	wantResp := &http.Response{StatusCode: http.StatusOK}
	wantErr := io.EOF

	m.DoFunc = func(r *http.Request) (*http.Response, error) {
		return wantResp, wantErr
	}

	resp, err := m.Do(req)
	if resp != wantResp {
		t.Errorf("expected response %v, got %v", wantResp, resp)
	}
	if err != wantErr {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
}

func TestMockHTTPClient_Snapshot_CallCount(t *testing.T) {
	m := &toolstest.MockHTTPClient{}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)

	const n = 5
	for i := 0; i < n; i++ {
		_, _ = m.Do(req)
	}

	callCount, _ := m.Snapshot()
	if callCount != n {
		t.Errorf("expected callCount %d, got %d", n, callCount)
	}
}

func TestMockHTTPClient_Snapshot_Requests(t *testing.T) {
	m := &toolstest.MockHTTPClient{}

	r1, _ := http.NewRequest(http.MethodGet, "https://one.example.com", nil)
	r2, _ := http.NewRequest(http.MethodPost, "https://two.example.com", nil)

	_, _ = m.Do(r1)
	_, _ = m.Do(r2)

	_, requests := m.Snapshot()
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests in snapshot, got %d", len(requests))
	}
	if requests[0] != r1 {
		t.Errorf("expected first request %v, got %v", r1, requests[0])
	}
	if requests[1] != r2 {
		t.Errorf("expected second request %v, got %v", r2, requests[1])
	}
}

func TestMockHTTPClient_BodyReadsAndCloses(t *testing.T) {
	m := &toolstest.MockHTTPClient{}
	body := "hello"
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(body))

	_, _ = m.Do(req)

	// After Do() returns, the body should be fully consumed (readable to EOF).
	remaining, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("unexpected error reading body after Do(): %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected empty body after Do(), got %q", string(remaining))
	}
}

func TestMockHTTPClient_NoBodyPanics(t *testing.T) {
	m := &toolstest.MockHTTPClient{}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)

	// Must not panic when req.Body is nil.
	_, err := m.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Snapshot still works and records the request.
	callCount, requests := m.Snapshot()
	if callCount != 1 {
		t.Errorf("expected callCount 1, got %d", callCount)
	}
	if len(requests) != 1 {
		t.Errorf("expected 1 request, got %d", len(requests))
	}
}
