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
	"testing/iotest"
	"time"
	"unicode/utf8"

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
	sm := security.NewSecurityManager(nil)

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
			tool := newnetworkTool(sm, mock)
			res, err := tool.HttpRequest(context.Background(), tt.args, nil)
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
	sm := security.NewSecurityManager(nil)

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
			tool := newnetworkTool(sm, mock)
			res, err := tool.ReadExternalDocs(context.Background(), tt.args, nil)
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
			sm := security.NewSecurityManager(&security.MockInteractor{Answer: input})

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
			res, err := m.sendTeamsMessage(context.Background(), tt.args, nil)
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
	sm := security.NewSecurityManager(nil)
	tool := newnetworkTool(sm, nil)
	if tool.client == nil {
		t.Error("newnetworkTool(sm, nil) should have initialized a default client")
	}
}

func TestNewTeamsManager(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := newteamsManager(sm, nil)
	if m.client == nil {
		t.Error("newteamsManager(sm, nil) should have initialized a default client")
	}
}

func TestRegister(t *testing.T) {
	r := registry.New()
	sm := security.NewSecurityManager(nil)
	if err := RegisterAll(r, sm, nil, ""); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

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
	sm := security.NewSecurityManager(nil)
	tool := newnetworkTool(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := tool.HttpRequest(context.Background(), map[string]interface{}{"method": 123}, nil)
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})
}

func TestReadExternalDocs_Errors(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	tool := newnetworkTool(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := tool.ReadExternalDocs(context.Background(), map[string]interface{}{"url": 123}, nil)
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})

	t.Run("Empty URL", func(t *testing.T) {
		_, err := tool.ReadExternalDocs(context.Background(), map[string]interface{}{"url": ""}, nil)
		if err == nil {
			t.Error("Expected error for empty URL")
		}
	})
}

func TestSendTeamsMessage_Errors(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := newteamsManager(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := m.sendTeamsMessage(context.Background(), map[string]interface{}{"message": 123}, nil)
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})

	t.Run("Empty Required Fields", func(t *testing.T) {
		_, err := m.sendTeamsMessage(context.Background(), map[string]interface{}{"message": "", "webhook_url": ""}, nil)
		if err == nil {
			t.Error("Expected error for empty fields")
		}
	})

	t.Run("Insecure URL", func(t *testing.T) {
		_, err := m.sendTeamsMessage(context.Background(), map[string]interface{}{
			"message":     "hello",
			"webhook_url": "http://insecure.com",
			"reason":      "testing",
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "must start with https://") {
			t.Errorf("Expected HTTPS validation error, got: %v", err)
		}
	})
}

func TestReadResponseWithLimit(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	tool := newnetworkTool(sm, nil)

	tests := []struct {
		name      string
		input     string
		limit     int64
		want      string
		wantTrunc bool
		wantErr   bool
	}{
		{
			name:      "Exact Limit",
			input:     strings.Repeat("A", 1024),
			limit:     1024,
			want:      strings.Repeat("A", 1024),
			wantTrunc: false,
		},
		{
			name:      "Over Limit",
			input:     strings.Repeat("B", 1025),
			limit:     1024,
			want:      strings.Repeat("B", 1024),
			wantTrunc: true,
		},
		{
			name:      "Under Limit",
			input:     "hello",
			limit:     100,
			want:      "hello",
			wantTrunc: false,
		},
		{
			name:    "IO Error",
			input:   "",
			limit:   100,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.ReadCloser
			if tt.name == "IO Error" {
				// Use an error reader
				body = io.NopCloser(iotest.ErrReader(errors.New("read error")))
			} else {
				body = io.NopCloser(strings.NewReader(tt.input))
			}
			got, truncated, err := tool.readResponseWithLimit(body, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("readResponseWithLimit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("readResponseWithLimit() got = %q, want %q", got, tt.want)
			}
			if truncated != tt.wantTrunc {
				t.Errorf("readResponseWithLimit() truncated = %v, want %v", truncated, tt.wantTrunc)
			}
		})
	}
}

func TestSanitizeHTML(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	tool := newnetworkTool(sm, nil)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Complex script tag",
			input: `<script type="text/javascript" src="malicious.js">alert(1)</script>`,
			want:  "",
		},
		{
			name:  "Style with attributes",
			input: `<style type="text/css">body { color: red; }</style>`,
			want:  "",
		},
		{
			name:  "Nested markup",
			input: `<div><p>Text <b>Bold</b></p></div>`,
			want:  "Text Bold",
		},
		{
			name:  "Whitespace collapse",
			input: "  Hello\n\n\tWorld  ",
			want:  "Hello World",
		},
		{
			name:  "Multiple newlines",
			input: "Line1\n\n\nLine2\n\nLine3",
			want:  "Line1 Line2 Line3",
		},
		{
			name:  "Mixed tags and spaces",
			input: "<p>Hello <span>world</span>!</p>",
			want:  "Hello world !",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.sanitizeHTML(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeHTML() got = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeartbeatConcurrency(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	tool := newnetworkTool(sm, nil)
	tool.heartbeatInterval = 100 * time.Millisecond

	// Use a buffered channel to avoid blocking
	hb := make(chan struct{}, 10)
	stop := tool.startHeartbeat(hb)

	// Wait for at least one heartbeat
	select {
	case <-hb:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for heartbeat")
	}

	// Call stop
	stop()

	// Drain any existing heartbeats
Loop:
	for {
		select {
		case <-hb:
		default:
			break Loop
		}
	}

	// Call stop again - should not panic
	stop()

	// Ensure no new heartbeats are sent
	time.Sleep(200 * time.Millisecond)
	select {
	case <-hb:
		t.Fatal("Unexpected heartbeat after stop")
	default:
		// OK
	}
}

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		max     int
		want    string
		wantLen int
	}{
		{
			name:    "ASCII exact",
			input:   "hello",
			max:     5,
			want:    "hello",
			wantLen: 5,
		},
		{
			name:    "ASCII truncate",
			input:   "hello world",
			max:     5,
			want:    "hello",
			wantLen: 5,
		},
		{
			name:    "Multi-byte rune boundary",
			input:   "café", // 'é' is 2 bytes
			max:     4,      // cut after 'c','a','f' (3 bytes), 'é' is 2 bytes, can't split
			want:    "caf",
			wantLen: 3,
		},
		{
			name:    "Emoji boundary",
			input:   "Hello 🎉 World", // 🎉 is 4 bytes
			max:     10,              // "Hello " is 6 bytes, 🎉 is 4 bytes -> exactly 10 bytes
			want:    "Hello 🎉",
			wantLen: 10,
		},
		{
			name:    "Emoji split",
			input:   "Hello 🎉 World",
			max:     9, // "Hello " (6) + 3 bytes of 🎉 (invalid)
			want:    "Hello ",
			wantLen: 6,
		},
		{
			name:    "Empty string",
			input:   "",
			max:     10,
			want:    "",
			wantLen: 0,
		},
		{
			name:    "Max zero",
			input:   "hello",
			max:     0,
			want:    "",
			wantLen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateUTF8(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
			if len(got) != tt.wantLen {
				t.Errorf("truncateUTF8(%q, %d) length = %d, want %d", tt.input, tt.max, len(got), tt.wantLen)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateUTF8(%q, %d) produced invalid UTF-8: %q", tt.input, tt.max, got)
			}
		})
	}
}

func TestTruncateUTF8Negative(t *testing.T) {
	got := truncateUTF8("hello", -1)
	if got != "" {
		t.Errorf("truncateUTF8(\"hello\", -1) = %q, want \"\"", got)
	}
}

func TestHttpRequest_ContextCancellation(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	ctx, cancel := context.WithCancel(context.Background())

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Wait for cancellation
			<-req.Context().Done()
			return nil, req.Context().Err()
		},
	}

	tool := newnetworkTool(sm, mock)
	tool.heartbeatInterval = 50 * time.Millisecond
	args := map[string]interface{}{
		"method": "GET",
		"url":    "https://example.com",
	}

	hb := make(chan struct{}, 10)

	// Start request in goroutine
	errCh := make(chan error, 1)
	go func() {
		_, err := tool.HttpRequest(ctx, args, hb)
		errCh <- err
	}()

	// Wait for at least one heartbeat
	select {
	case <-hb:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for first heartbeat")
	}

	// Cancel context
	cancel()

	// Wait for error
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for HttpRequest to return after cancellation")
	}

	// Drain heartbeat
Loop:
	for {
		select {
		case <-hb:
		default:
			break Loop
		}
	}

	// Verify no new heartbeats
	time.Sleep(100 * time.Millisecond)
	select {
	case <-hb:
		t.Fatal("Heartbeat still running after cancellation")
	default:
		// OK
	}
}

func TestReadExternalDocs_ContextCancellation(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	ctx, cancel := context.WithCancel(context.Background())

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Wait for cancellation
			<-req.Context().Done()
			return nil, req.Context().Err()
		},
	}

	tool := newnetworkTool(sm, mock)
	tool.heartbeatInterval = 50 * time.Millisecond
	args := map[string]interface{}{
		"url": "https://example.com/docs",
	}

	hb := make(chan struct{}, 10)

	// Start request in goroutine
	errCh := make(chan error, 1)
	go func() {
		_, err := tool.ReadExternalDocs(ctx, args, hb)
		errCh <- err
	}()

	// Wait for at least one heartbeat
	select {
	case <-hb:
		// Good
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for first heartbeat")
	}

	// Cancel context
	cancel()

	// Wait for error
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for ReadExternalDocs to return after cancellation")
	}

	// Drain heartbeat
Loop:
	for {
		select {
		case <-hb:
		default:
			break Loop
		}
	}

	// Verify no new heartbeats
	time.Sleep(100 * time.Millisecond)
	select {
	case <-hb:
		t.Fatal("Heartbeat still running after cancellation")
	default:
		// OK
	}
}
