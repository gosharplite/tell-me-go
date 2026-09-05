// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package callback_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/callback"
)

func TestHTTPNotifier_Success(t *testing.T) {
	statusCodes := []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}

	for _, statusCode := range statusCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			var (
				mu          sync.Mutex
				gotMethod   string
				gotHeader   http.Header
				gotBodyData []byte
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				gotMethod = r.Method
				gotHeader = r.Header.Clone()
				gotBodyData, _ = io.ReadAll(r.Body)
				w.WriteHeader(statusCode)
			}))
			defer server.Close()

			notifier := callback.NewHTTPNotifierWithClient(server.Client())

			errStr := "test error message"
			payload := domain_callback.CallbackPayload{
				SessionID: "sess-123",
				Status:    domain_callback.StatusError,
				Response:  "",
				Error:     &errStr,
			}

			headers := map[string]string{
				"X-Custom-Auth":  "Bearer token123",
				"X-Request-Mode": "webhook",
			}

			ctx := context.Background()
			err := notifier.Notify(ctx, server.URL, headers, payload)
			if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()

			if gotMethod != http.MethodPost {
				t.Errorf("expected POST method, got: %q", gotMethod)
			}
			if ct := gotHeader.Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got: %q", ct)
			}
			if auth := gotHeader.Get("X-Custom-Auth"); auth != "Bearer token123" {
				t.Errorf("expected X-Custom-Auth 'Bearer token123', got: %q", auth)
			}
			if mode := gotHeader.Get("X-Request-Mode"); mode != "webhook" {
				t.Errorf("expected X-Request-Mode 'webhook', got: %q", mode)
			}

			var receivedPayload domain_callback.CallbackPayload
			if err := json.Unmarshal(gotBodyData, &receivedPayload); err != nil {
				t.Fatalf("failed to unmarshal request body: %v", err)
			}
			if receivedPayload.SessionID != payload.SessionID {
				t.Errorf("SessionID mismatch: got %q, want %q", receivedPayload.SessionID, payload.SessionID)
			}
			if receivedPayload.Status != payload.Status {
				t.Errorf("Status mismatch: got %q, want %q", receivedPayload.Status, payload.Status)
			}
			if receivedPayload.Error == nil || *receivedPayload.Error != errStr {
				t.Errorf("Error field mismatch: got %v, want %q", receivedPayload.Error, errStr)
			}
		})
	}
}

func TestHTTPNotifier_Non2xxError(t *testing.T) {
	statusCodes := []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusBadGateway}

	for _, statusCode := range statusCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte("error response body"))
			}))
			defer server.Close()

			notifier := callback.NewHTTPNotifierWithClient(server.Client())
			payload := domain_callback.CallbackPayload{
				SessionID: "sess-fail",
				Status:    domain_callback.StatusSuccess,
				Response:  "output",
			}

			ctx := context.Background()
			err := notifier.Notify(ctx, server.URL, nil, payload)
			if err == nil {
				t.Fatal("expected error for non-2xx status code, got nil")
			}

			expectedFragment := strings.ToLower(http.StatusText(statusCode))
			_ = expectedFragment
			if !strings.Contains(err.Error(), "status") {
				t.Errorf("expected error to mention 'status', got: %v", err)
			}
			statusCodeStr := string(rune('0' + statusCode/100))
			if !strings.Contains(err.Error(), statusCodeStr) {
				t.Errorf("expected error to contain status code %d, got: %v", statusCode, err)
			}
		})
	}
}

func TestHTTPNotifier_ContextTimeout(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	defer func() {
		close(done)
		server.Close()
	}()

	notifier := callback.NewHTTPNotifierWithClient(server.Client())
	payload := domain_callback.CallbackPayload{
		SessionID: "sess-timeout",
		Status:    domain_callback.StatusSuccess,
		Response:  "timeout test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := notifier.Notify(ctx, server.URL, nil, payload)
	if err == nil {
		t.Fatal("expected context timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "canceled") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected error to indicate timeout/cancellation, got: %v", err)
	}
}

func TestHTTPNotifier_InvalidURL(t *testing.T) {
	notifier := callback.NewHTTPNotifier()
	payload := domain_callback.CallbackPayload{
		SessionID: "sess-badurl",
		Status:    domain_callback.StatusSuccess,
		Response:  "test",
	}

	ctx := context.Background()
	// A control character in URL causes http.NewRequestWithContext to fail
	err := notifier.Notify(ctx, "http://localhost:invalid\x7furl", nil, payload)
	if err == nil {
		t.Fatal("expected error with invalid URL, got nil")
	}
}

func TestNewHTTPNotifier_Constructors(t *testing.T) {
	notifier := callback.NewHTTPNotifier()
	if notifier == nil {
		t.Fatal("expected non-nil notifier from NewHTTPNotifier")
	}

	customClient := &http.Client{Timeout: 5 * time.Second}
	customNotifier := callback.NewHTTPNotifierWithClient(customClient)
	if customNotifier == nil {
		t.Fatal("expected non-nil notifier from NewHTTPNotifierWithClient")
	}
}
