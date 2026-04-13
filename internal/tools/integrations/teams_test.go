// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
)

func TestTeamsManager_SendTeamsMessage(t *testing.T) {
	sm := &testutil.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name       string
		args       map[string]interface{}
		mockResp   *http.Response
		mockErr    error
		wantInText []string
		wantErr    bool
	}{
		{
			name: "Success",
			args: map[string]interface{}{
				"webhook_url": "https://example.com/webhook",
				"message":     "Hello Teams",
				"reason":      "testing",
			},
			mockResp: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("OK")),
			},
			wantInText: []string{"Successfully sent message to Teams", "200 OK"},
		},
		{
			name: "Unmarshal Error",
			args: map[string]interface{}{
				"webhook_url": 123,
			},
			wantErr: true,
		},
		{
			name: "Missing Webhook URL",
			args: map[string]interface{}{
				"message": "Hello",
			},
			wantErr: true,
		},
		{
			name: "Insecure Webhook URL",
			args: map[string]interface{}{
				"webhook_url": "http://insecure.com",
				"message":     "Hello",
			},
			wantErr: true,
		},
		{
			name: "HTTP Request Creation Error",
			args: map[string]interface{}{
				"webhook_url": "https://example.com/webhook",
				"message":     "Hello",
			},
			mockErr:    errors.New("request creation error"),
			wantErr:    false, // Error is returned as Text in ToolResult
			wantInText: []string{"failed to send message"},
		},
		{
			name: "Non-2xx Status Code",
			args: map[string]interface{}{
				"webhook_url": "https://example.com/webhook",
				"message":     "Hello",
			},
			mockResp: &http.Response{
				Status:     "403 Forbidden",
				StatusCode: 403,
				Body:       io.NopCloser(strings.NewReader("Forbidden")),
			},
			wantInText: []string{"failed to send message to Teams", "403 Forbidden"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return tt.mockResp, nil
				},
			}
			m := newteamsManager(sm, mock)
			res, err := m.sendTeamsMessage(context.Background(), tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("sendTeamsMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantInText {
					if !strings.Contains(res.Text, want) {
						t.Errorf("sendTeamsMessage() text = %q, want to contain %q", res.Text, want)
					}
				}
			}
		})
	}
}

func TestTeamsManager_Timeout(t *testing.T) {
	sm := &testutil.MockSecurityManager{AllowAll: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(100 * time.Millisecond):
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("OK"))}, nil
			}
		},
	}

	m := newteamsManager(sm, mock)
	args := map[string]interface{}{
		"webhook_url": "https://example.com/webhook",
		"message":     "Hello",
	}

	res, err := m.sendTeamsMessage(ctx, args, nil)
	if err != nil {
		t.Fatalf("Unexpected error from sendTeamsMessage: %v", err)
	}
	if !strings.Contains(res.Text, "context deadline exceeded") {
		t.Errorf("Expected timeout error in response text, got: %q", res.Text)
	}
}

func TestBuildTeamsRequestBody(t *testing.T) {
	body, err := buildTeamsRequestBody("hello", "reason")
	if err != nil {
		t.Fatalf("buildTeamsRequestBody failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Failed to unmarshal body: %v", err)
	}
	if payload["type"] != "AdaptiveCard" {
		t.Errorf("Expected type AdaptiveCard, got %v", payload["type"])
	}
}

func TestPostToWebhook_RequestError(t *testing.T) {
	sm := &testutil.MockSecurityManager{AllowAll: true}
	m := newteamsManager(sm, nil)

	// Use an invalid URL that will cause NewRequestWithContext to fail if possible
	// Actually, NewRequestWithContext rarely fails for just URL.
	// But it fails if method is invalid.
	_, err := m.postToWebhook(context.Background(), "%%%", nil)
	if err == nil {
		t.Error("Expected error for invalid URL in postToWebhook")
	}
}

func TestNewTeamsManager_DefaultClient(t *testing.T) {
	sm := &testutil.MockSecurityManager{AllowAll: true}
	m := newteamsManager(sm, nil)
	if m.client == nil {
		t.Error("Expected default client to be initialized")
	}
}
