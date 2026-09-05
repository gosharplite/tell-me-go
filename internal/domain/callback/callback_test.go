// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package callback_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/callback"
)

func TestCallbackPayload_JSONSuccess(t *testing.T) {
	payload := callback.CallbackPayload{
		SessionID: "session-12345",
		Status:    callback.StatusSuccess,
		Response:  "result content",
		Error:     nil,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"error":null`) {
		t.Errorf("expected JSON to contain '\"error\":null', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"status":"success"`) {
		t.Errorf("expected JSON to contain '\"status\":\"success\"', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"response":"result content"`) {
		t.Errorf("expected JSON to contain '\"response\":\"result content\"', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"session_id":"session-12345"`) {
		t.Errorf("expected JSON to contain '\"session_id\":\"session-12345\"', got: %s", jsonStr)
	}
}

func TestCallbackPayload_JSONError(t *testing.T) {
	errStr := "something went wrong"
	payload := callback.CallbackPayload{
		SessionID: "session-error-1",
		Status:    callback.StatusError,
		Response:  "",
		Error:     &errStr,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"error":"something went wrong"`) {
		t.Errorf("expected JSON to contain '\"error\":\"something went wrong\"', got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"status":"error"`) {
		t.Errorf("expected JSON to contain '\"status\":\"error\"', got: %s", jsonStr)
	}
}

func TestCallbackPayload_RoundTrip(t *testing.T) {
	errStr := "network failure"
	cases := []struct {
		name    string
		payload callback.CallbackPayload
	}{
		{
			name: "success payload",
			payload: callback.CallbackPayload{
				SessionID: "sess-abc",
				Status:    callback.StatusSuccess,
				Response:  "all done",
				Error:     nil,
			},
		},
		{
			name: "error payload",
			payload: callback.CallbackPayload{
				SessionID: "sess-def",
				Status:    callback.StatusError,
				Response:  "",
				Error:     &errStr,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var unmarshaled callback.CallbackPayload
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if unmarshaled.SessionID != tc.payload.SessionID {
				t.Errorf("SessionID mismatch: got %q, want %q", unmarshaled.SessionID, tc.payload.SessionID)
			}
			if unmarshaled.Status != tc.payload.Status {
				t.Errorf("Status mismatch: got %q, want %q", unmarshaled.Status, tc.payload.Status)
			}
			if unmarshaled.Response != tc.payload.Response {
				t.Errorf("Response mismatch: got %q, want %q", unmarshaled.Response, tc.payload.Response)
			}
			if tc.payload.Error == nil {
				if unmarshaled.Error != nil {
					t.Errorf("Error should be nil, got %q", *unmarshaled.Error)
				}
			} else {
				if unmarshaled.Error == nil {
					t.Fatal("Error should not be nil")
				}
				if *unmarshaled.Error != *tc.payload.Error {
					t.Errorf("Error mismatch: got %q, want %q", *unmarshaled.Error, *tc.payload.Error)
				}
			}
		})
	}
}

type mockNotifier struct {
	lastURL     string
	lastHeaders map[string]string
	lastPayload callback.CallbackPayload
	errToReturn error
}

func (m *mockNotifier) Notify(ctx context.Context, url string, headers map[string]string, payload callback.CallbackPayload) error {
	m.lastURL = url
	m.lastHeaders = headers
	m.lastPayload = payload
	return m.errToReturn
}

func TestCallbackNotifier_InterfaceSatisfaction(t *testing.T) {
	var notifier callback.CallbackNotifier = &mockNotifier{}
	ctx := context.Background()
	payload := callback.CallbackPayload{
		SessionID: "s-1",
		Status:    callback.StatusSuccess,
		Response:  "ok",
	}

	err := notifier.Notify(ctx, "http://localhost:8080/cb", map[string]string{"X-Test": "val"}, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := notifier.(*mockNotifier)
	if m.lastURL != "http://localhost:8080/cb" {
		t.Errorf("URL mismatch: got %q", m.lastURL)
	}
	if m.lastHeaders["X-Test"] != "val" {
		t.Errorf("Header mismatch: got %v", m.lastHeaders)
	}
	if m.lastPayload.SessionID != "s-1" {
		t.Errorf("Payload mismatch: got %v", m.lastPayload)
	}
}
