// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package callback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewHTTPNotifierWithClient_NilClientFallsBackToDefault covers the
// nil-client fallback branch in NewHTTPNotifierWithClient
// (http_notifier.go:34-38): a nil *http.Client must be replaced with the
// default 15s-timeout client. White-box (package callback) because the
// client field is unexported; the external http_notifier_test.go never
// exercises NewHTTPNotifierWithClient(nil).
func TestNewHTTPNotifierWithClient_NilClientFallsBackToDefault(t *testing.T) {
	n := NewHTTPNotifierWithClient(nil)
	require.NotNil(t, n)
	require.NotNil(t, n.client)
	assert.Equal(t, 15*time.Second, n.client.Timeout)

	// Functional proof: the fallback client must actually deliver a payload.
	// Guards against a future regression where the fallback constructs a
	// zero-timeout (or otherwise broken) client that still passes the field
	// assert above. Local httptest round-trip — deterministic, no sleeps.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload := domain_callback.CallbackPayload{
		SessionID: "sess-nil-fallback",
		Status:    domain_callback.StatusSuccess,
		Response:  "delivered via fallback client",
	}
	err := n.Notify(context.Background(), server.URL, nil, payload)
	assert.NoError(t, err)
}

// TestNewHTTPNotifier_DefaultTimeout pins the sibling constructor's default to
// the same ADR-076 D6 contract: NewHTTPNotifier() must always produce a client
// with the 15s single-POST hard timeout.
func TestNewHTTPNotifier_DefaultTimeout(t *testing.T) {
	n := NewHTTPNotifier()
	require.NotNil(t, n)
	require.NotNil(t, n.client)
	assert.Equal(t, 15*time.Second, n.client.Timeout)
}
