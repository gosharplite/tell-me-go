// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

func TestHttpRequest(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))

	tests := []struct {
		name       string
		args       map[string]interface{}
		mockResp   *http.Response
		mockErr    error
		wantInText []string
		wantErr    bool
	}{
		{
			name: "Success GET",
			args: map[string]interface{}{
				"method": "GET",
				"url":    "https://example.com",
			},
			mockResp: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("Hello World")),
			},
			wantInText: []string{"Status: 200 OK", "Content-Type: text/plain", "Hello World"},
		},
		{
			name: "Success POST with body and headers",
			args: map[string]interface{}{
				"method":  "POST",
				"url":     "https://example.com",
				"headers": map[string]string{"X-Test": "value"},
				"body":    "request body",
			},
			mockResp: &http.Response{
				Status:     "201 Created",
				StatusCode: 201,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("Created")),
			},
			wantInText: []string{"Status: 201 Created", "Created"},
		},
		{
			name: "Large Body Truncation",
			args: map[string]interface{}{
				"method": "GET",
				"url":    "https://example.com",
			},
			mockResp: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(strings.Repeat("A", 5*1024*1024+10))),
			},
			wantInText: []string{"... (truncated due to size limit)"},
		},
		{
			name: "Request Failure",
			args: map[string]interface{}{
				"method": "GET",
				"url":    "https://invalid",
			},
			mockErr: io.EOF,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return tt.mockResp, tt.mockErr
				},
			}
			tool := NewNetworkTool(sm, mock)
			res, err := tool.HttpRequest(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("HttpRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantInText {
					if !strings.Contains(res.Text, want) {
						t.Errorf("HttpRequest() text = %v, want to contain %v", res.Text, want)
					}
				}
			}
		})
	}
}

func TestReadExternalDocs(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))

	tests := []struct {
		name       string
		args       map[string]interface{}
		mockResp   *http.Response
		mockErr    error
		wantInText []string
		wantErr    bool
	}{
		{
			name: "Success HTML stripping",
			args: map[string]interface{}{
				"url": "https://example.com/docs",
			},
			mockResp: &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`
					<html>
						<head><style>body { color: red; }</style></head>
						<body>
							<h1>Title</h1>
							<script>alert('hi');</script>
							<p>This is a <b>test</b>.</p>
						</body>
					</html>
				`)),
			},
			wantInText: []string{"Title", "This is a test"},
		},
		{
			name: "Status Not OK",
			args: map[string]interface{}{
				"url": "https://example.com/404",
			},
			mockResp: &http.Response{
				StatusCode: 404,
				Body:       io.NopCloser(strings.NewReader("Not Found")),
			},
			wantErr: true,
		},
		{
			name: "Content Truncation",
			args: map[string]interface{}{
				"url": "https://example.com/large",
			},
			mockResp: &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(strings.Repeat("A ", 6000))),
			},
			wantInText: []string{"... (truncated)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return tt.mockResp, tt.mockErr
				},
			}
			tool := NewNetworkTool(sm, mock)
			res, err := tool.ReadExternalDocs(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadExternalDocs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantInText {
					if !strings.Contains(res.Text, want) {
						t.Errorf("ReadExternalDocs() text = %v, want to contain %v", res.Text, want)
					}
				}
				if strings.Contains(res.Text, "<style>") || strings.Contains(res.Text, "<script>") {
					t.Errorf("ReadExternalDocs() text contains HTML tags: %v", res.Text)
				}
			}
		})
	}
}

func TestSendTeamsMessage(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]interface{}
		mockResp   *http.Response
		mockErr    error
		confirm    bool
		wantInText []string
		wantErr    bool
	}{
		{
			name: "Success with confirmation",
			args: map[string]interface{}{
				"webhook_url": "https://outlook.office.com/webhook/...",
				"message":     "Hello Teams",
				"reason":      "testing",
			},
			mockResp: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader("OK")),
			},
			confirm:    true,
			wantInText: []string{"Successfully sent message to Teams", "200 OK"},
		},
		{
			name: "Cancelled by user",
			args: map[string]interface{}{
				"webhook_url": "https://outlook.office.com/webhook/...",
				"message":     "Hello Teams",
				"reason":      "testing",
			},
			confirm:    false,
			wantInText: []string{"Action cancelled by user."},
		},
		{
			name: "Failed request",
			args: map[string]interface{}{
				"webhook_url": "https://outlook.office.com/webhook/...",
				"message":     "Hello Teams",
				"reason":      "testing",
			},
			mockResp: &http.Response{
				Status:     "500 Internal Server Error",
				StatusCode: 500,
				Body:       io.NopCloser(strings.NewReader("Error")),
			},
			confirm:    true,
			wantInText: []string{"failed to send message to Teams", "500 Internal Server Error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "n\n"
			if tt.confirm {
				input = "y\n"
			}
			sm := security.NewSecurityManager(strings.NewReader(input))

			mock := &mockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					// Verify payload
					var payload map[string]interface{}
					body, err := io.ReadAll(req.Body)
					if err != nil {
						t.Errorf("Failed to read request body: %v", err)
						return nil, err
					}
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Errorf("Failed to unmarshal request body: %v", err)
						return nil, err
					}
					if payload["type"] != "AdaptiveCard" {
						t.Errorf("Expected AdaptiveCard payload, got %v", payload["type"])
					}
					return tt.mockResp, tt.mockErr
				},
			}

			m := &teamsManager{sm: sm, client: mock}
			res, err := m.sendTeamsMessage(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("sendTeamsMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantInText {
					if !strings.Contains(res.Text, want) {
						t.Errorf("sendTeamsMessage() text = %v, want to contain %v", res.Text, want)
					}
				}
			}
		})
	}
}

func TestNewNetworkTool(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))
	tool := NewNetworkTool(sm, nil)
	if tool.client == nil {
		t.Error("NewNetworkTool(sm, nil) should have initialized a default client")
	}
}

func TestNewTeamsManager(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))
	m := NewTeamsManager(sm, nil)
	if m.client == nil {
		t.Error("NewTeamsManager(sm, nil) should have initialized a default client")
	}
}

func TestRegister(t *testing.T) {
	r := registry.New()
	sm := security.NewSecurityManager(strings.NewReader(""))
	RegisterAll(r, sm, nil, "")

	decls := r.GetDeclarations()
	found := make(map[string]bool)
	for _, d := range decls {
		found[d.Name] = true
	}

	expected := []string{
		"read_external_docs", "http_request", "send_teams_message",
		"create_image", "read_image",
		"confluence_search", "confluence_read", "confluence_write",
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("Tool %s not registered", name)
		}
	}
}

func TestHttpRequest_Errors(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))
	tool := NewNetworkTool(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := tool.HttpRequest(context.Background(), map[string]interface{}{"method": 123})
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})
}

func TestReadExternalDocs_Errors(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))
	tool := NewNetworkTool(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := tool.ReadExternalDocs(context.Background(), map[string]interface{}{"url": 123})
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})

	t.Run("Empty URL", func(t *testing.T) {
		_, err := tool.ReadExternalDocs(context.Background(), map[string]interface{}{"url": ""})
		if err == nil {
			t.Error("Expected error for empty URL")
		}
	})
}

func TestSendTeamsMessage_Errors(t *testing.T) {
	sm := security.NewSecurityManager(strings.NewReader(""))
	m := NewTeamsManager(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := m.sendTeamsMessage(context.Background(), map[string]interface{}{"message": 123})
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})

	t.Run("Empty Required Fields", func(t *testing.T) {
		_, err := m.sendTeamsMessage(context.Background(), map[string]interface{}{"message": "", "webhook_url": ""})
		if err == nil {
			t.Error("Expected error for empty fields")
		}
	})

	t.Run("Insecure URL", func(t *testing.T) {
		_, err := m.sendTeamsMessage(context.Background(), map[string]interface{}{
			"message":     "hello",
			"webhook_url": "http://insecure.com",
			"reason":      "testing",
		})
		if err == nil || !strings.Contains(err.Error(), "must start with https://") {
			t.Errorf("Expected HTTPS validation error, got: %v", err)
		}
	})
}
