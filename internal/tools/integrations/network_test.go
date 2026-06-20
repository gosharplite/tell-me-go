// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"
	"unicode/utf8"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
}

type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

func TestHttpRequest(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

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
		{
			name: "Response body read error",
			args: map[string]interface{}{
				"method": "GET",
				"url":    "https://example.com",
			},
			mockResp: &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       io.NopCloser(iotest.ErrReader(errors.New("simulated body read error"))),
			},
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
	sm := &toolstest.MockSecurityManager{AllowAll: true}

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
		{
			name: "Response body read error",
			args: map[string]interface{}{
				"url": "https://example.com/docs",
			},
			mockResp: &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(iotest.ErrReader(errors.New("simulated body read error"))),
			},
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

func TestNewNetworkTool(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	tool := newnetworkTool(sm, nil)
	if tool.client == nil {
		t.Error("newnetworkTool(sm, nil) should have initialized a default client")
	}
}

func TestRegister(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	r := registry.New()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	if err := RegisterAll(r, nil, sm, nil, ""); err != nil {
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
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	tool := newnetworkTool(sm, nil)

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := tool.HttpRequest(context.Background(), map[string]interface{}{"method": 123}, nil)
		if err == nil {
			t.Error("Expected error for invalid args")
		}
	})
}

// ── Request creation error path tests (Issue #433) ──

func TestHttpRequest_RequestCreationError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	tool := newnetworkTool(sm, &mockHTTPClient{})

	// http.NewRequestWithContext fails on malformed URLs
	_, err := tool.HttpRequest(context.Background(), map[string]interface{}{
		"method": "GET",
		"url":    "://invalid-url",
	}, nil)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("expected 'failed to create request', got: %v", err)
	}
}

func TestExecuteRequest(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("valid GET", func(t *testing.T) {
		wantResp := &http.Response{
			Status:     "200 OK",
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("OK")),
		}
		mock := &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					t.Errorf("expected GET, got %s", req.Method)
				}
				if req.URL.String() != "https://example.com" {
					t.Errorf("expected https://example.com, got %s", req.URL.String())
				}
				return wantResp, nil
			},
		}
		tool := newnetworkTool(sm, mock)
		resp, stopHB, err := tool.executeRequest(context.Background(), "GET", "https://example.com", "", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != wantResp {
			t.Error("response mismatch")
		}
		if stopHB != nil {
			t.Error("stopHB should be nil when hb channel is nil")
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		tool := newnetworkTool(sm, &mockHTTPClient{})
		_, stopHB, err := tool.executeRequest(context.Background(), "GET", "://invalid-url", "", nil, nil)
		if err == nil {
			t.Error("expected error for invalid URL")
		}
		if !strings.Contains(err.Error(), "failed to create request") {
			t.Errorf("expected 'failed to create request', got: %v", err)
		}
		if stopHB != nil {
			t.Error("stopHB should be nil on error")
		}
	})

	t.Run("client error propagates", func(t *testing.T) {
		mock := &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
		}
		tool := newnetworkTool(sm, mock)
		_, stopHB, err := tool.executeRequest(context.Background(), "GET", "https://example.com", "", nil, nil)
		if err == nil {
			t.Error("expected error from client")
		}
		if !strings.Contains(err.Error(), "request failed") {
			t.Errorf("expected 'request failed', got: %v", err)
		}
		if stopHB != nil {
			t.Error("stopHB should be nil on client error")
		}
	})

	t.Run("POST with body and headers", func(t *testing.T) {
		wantResp := &http.Response{
			Status:     "201 Created",
			StatusCode: 201,
			Body:       io.NopCloser(strings.NewReader("Created")),
		}
		mock := &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.Method != "POST" {
					t.Errorf("expected POST, got %s", req.Method)
				}
				if req.Header.Get("X-Custom") != "value" {
					t.Errorf("expected X-Custom: value, got %s", req.Header.Get("X-Custom"))
				}
				body, _ := io.ReadAll(req.Body)
				if string(body) != "request body" {
					t.Errorf("expected body 'request body', got %q", string(body))
				}
				return wantResp, nil
			},
		}
		tool := newnetworkTool(sm, mock)
		resp, stopHB, err := tool.executeRequest(context.Background(), "POST", "https://example.com", "request body", map[string]string{"X-Custom": "value"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != wantResp {
			t.Error("response mismatch")
		}
		if stopHB != nil {
			t.Error("stopHB should be nil when hb channel is nil")
		}
	})

	t.Run("with heartbeat channel", func(t *testing.T) {
		wantResp := &http.Response{
			Status:     "200 OK",
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("OK")),
		}
		mock := &mockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return wantResp, nil
			},
		}
		tool := newnetworkTool(sm, mock)
		tool.heartbeatInterval = 10 * time.Millisecond
		hb := make(chan struct{}, 1)
		resp, stopHB, err := tool.executeRequest(context.Background(), "GET", "https://example.com", "", nil, hb)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stopHB == nil {
			t.Error("stopHB should not be nil when hb channel is provided")
		}
		stopHB()
		// Drain heartbeat
		select {
		case <-hb:
		default:
		}
		if resp != wantResp {
			t.Error("response mismatch")
		}
	})
}

func TestReadExternalDocs_RequestCreationError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	tool := newnetworkTool(sm, &mockHTTPClient{})

	_, err := tool.ReadExternalDocs(context.Background(), map[string]interface{}{
		"url": "://invalid-url",
	}, nil)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("expected 'failed to create request', got: %v", err)
	}
}

func TestReadExternalDocs_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
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

func TestReadResponseWithLimit(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
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

func TestFormatHTTPResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		status       string
		headers      http.Header
		body         string
		truncated    bool
		wantContains []string
	}{
		{
			name:         "success with headers and body",
			status:       "200 OK",
			headers:      http.Header{"Content-Type": []string{"text/plain"}},
			body:         "Hello World",
			truncated:    false,
			wantContains: []string{"Status: 200 OK", "Content-Type: text/plain", "Body:", "Hello World"},
		},
		{
			name:         "truncated body",
			status:       "200 OK",
			headers:      http.Header{},
			body:         "data",
			truncated:    true,
			wantContains: []string{"... (truncated due to size limit)"},
		},
		{
			name:         "multiple header values",
			status:       "200 OK",
			headers:      http.Header{"X-Custom": []string{"a", "b"}},
			body:         "",
			truncated:    false,
			wantContains: []string{"X-Custom: a, b"},
		},
		{
			name:         "empty everything",
			status:       "",
			headers:      http.Header{},
			body:         "",
			truncated:    false,
			wantContains: []string{"Status: ", "Headers:", "Body:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatHTTPResponse(tt.status, tt.headers, tt.body, tt.truncated)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("formatHTTPResponse() missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestSanitizeHTML(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
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
			name:  "Case insensitive tags",
			input: `<SCRIPT>alert(1)</SCRIPT><StYlE>body{}</sTyLe>Keep this.`,
			want:  "Keep this.",
		},
		{
			name:  "Nested forbidden tags",
			input: `<div>Outer<script>Inner<style>VeryInner</style>BackToInner</script>StillOuter</div>`,
			want:  "Outer StillOuter",
		},
		{
			name:  "Unclosed forbidden tag",
			input: `<script>alert(1) <b>Bold</b>`,
			want:  "",
		},
		{
			name:  "Deeply nested HTML",
			input: `<div><span><ul><li>Item 1</li><li>Item 2</li></ul></span></div>`,
			want:  "Item 1 Item 2",
		},
		{
			name:  "Malformed HTML with script",
			input: `<script> <div> </script> <span>Visible</span>`,
			want:  "Visible",
		},
		{
			name:  "Self-closing forbidden tags",
			input: `<script src="foo.js" />Next text`,
			want:  "Next text",
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
		{
			name:  "HTML entities",
			input: "<div>&lt;b&gt;Hello &amp; World&lt;/b&gt;</div>",
			want:  "<b>Hello & World</b>",
		},
		{
			name:  "Malformed tag with attribute containing >",
			input: `<img src="x" alt=">"> This text should be correctly handled.`,
			want:  `This text should be correctly handled.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.sanitizeHTML(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeHTML() [%s] got = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestHeartbeatConcurrency(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	tool := newnetworkTool(sm, nil)
	tool.heartbeatInterval = 10 * time.Millisecond

	// Use a buffered channel to avoid blocking
	hb := make(chan struct{}, 10)
	stop := tool.startHeartbeat(hb)

	// Wait for at least one heartbeat
	select {
	case <-hb:
		// Good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for heartbeat")
	}

	// Call stop - this now waits for the goroutine to exit.
	stop()

	// Drain any existing heartbeats that were sent before stop returned.
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

	// Ensure no new heartbeats are sent.
	// No sleep needed because stop() guaranteed goroutine exit.
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
	args := map[string]interface{}{
		"method": "GET",
		"url":    "https://example.com",
	}
	runNetworkCancellationTest(t, "HttpRequest", args)
}

func TestReadExternalDocs_ContextCancellation(t *testing.T) {
	args := map[string]interface{}{
		"url": "https://example.com/docs",
	}
	runNetworkCancellationTest(t, "ReadExternalDocs", args)
}

func runNetworkCancellationTest(t *testing.T, actionName string, args map[string]interface{}) {
	t.Helper()
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		},
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	tool := newnetworkTool(sm, mock)
	tool.heartbeatInterval = 50 * time.Millisecond
	hb := make(chan struct{}, 10)

	errCh := make(chan error, 1)
	go func() {
		var err error
		if actionName == "HttpRequest" {
			_, err = tool.HttpRequest(ctx, args, hb)
		} else {
			_, err = tool.ReadExternalDocs(ctx, args, hb)
		}
		errCh <- err
	}()

	verifyFirstHeartbeat(t, hb, actionName)
	cancel()
	verifyCancellationError(t, errCh, actionName)
	drainAndVerifyHeartbeatStopped(t, hb)
}

func verifyFirstHeartbeat(t *testing.T, hb <-chan struct{}, actionName string) {
	t.Helper()
	select {
	case <-hb:
	case <-time.After(2 * time.Second):
		t.Fatalf("Timeout waiting for first heartbeat from %s", actionName)
	}
}

func verifyCancellationError(t *testing.T, errCh <-chan error, actionName string) {
	t.Helper()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Timeout waiting for %s to return after cancellation", actionName)
	}
}

func drainAndVerifyHeartbeatStopped(t *testing.T, hb <-chan struct{}) {
	t.Helper()
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
	select {
	case <-hb:
		t.Fatal("Heartbeat still running after cancellation")
	default:
		// OK
	}
}

func TestRegisterNetwork_ErrorPaths(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("first RegisterToToolkitWithOptions fails", func(t *testing.T) {
		r := newFaultyRegistry(registry.New(), 0)
		tool := newnetworkTool(sm, nil)
		err := registerNetwork(r, tool)
		if err == nil {
			t.Fatal("expected error on first registration failure")
		}
		if !strings.Contains(err.Error(), "simulated registration failure") {
			t.Errorf("expected simulated error, got: %v", err)
		}
	})

	t.Run("second RegisterToToolkitWithOptions fails", func(t *testing.T) {
		r := newFaultyRegistry(registry.New(), 1)
		tool := newnetworkTool(sm, nil)
		err := registerNetwork(r, tool)
		if err == nil {
			t.Fatal("expected error on second registration failure")
		}
		if !strings.Contains(err.Error(), "simulated registration failure") {
			t.Errorf("expected simulated error, got: %v", err)
		}
	})
}
