package llmerr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{
		Status: 403,
		Body:   "Forbidden",
	}

	got := err.Error()
	want := "api error (status 403): Forbidden"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name          string
		input         error
		expectedWrap  error
		containsMatch string
	}{
		{
			name:  "Nil error",
			input: nil,
		},
		{
			name:         "Domain Auth error",
			input:        llm.ErrAuth,
			expectedWrap: llm.ErrAuth,
		},
		{
			name:         "Domain Transient error",
			input:        llm.ErrTransient,
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "Domain Terminal error",
			input:        llm.ErrTerminal,
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "HTTP 401 via APIError",
			input:        &APIError{Status: 401, Body: "Unauthorized"},
			expectedWrap: llm.ErrAuth,
		},
		{
			name:         "HTTP 429 via APIError",
			input:        &APIError{Status: 429, Body: "Rate limit exceeded"},
			expectedWrap: llm.ErrRateLimit,
		},
		{
			name:         "HTTP 408 via APIError",
			input:        &APIError{Status: 408, Body: "Request Timeout"},
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "HTTP 503 via APIError",
			input:        &APIError{Status: 503, Body: "Service Unavailable"},
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "HTTP 499 via APIError",
			input:        &APIError{Status: 499, Body: "Client Closed Request"},
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "HTTP 400 via APIError",
			input:        &APIError{Status: 400, Body: "Bad Request"},
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "gRPC Unauthenticated",
			input:        status.Error(codes.Unauthenticated, "unauthenticated"),
			expectedWrap: llm.ErrAuth,
		},
		{
			name:         "gRPC ResourceExhausted",
			input:        status.Error(codes.ResourceExhausted, "quota exceeded"),
			expectedWrap: llm.ErrRateLimit,
		},
		{
			name:         "gRPC Unavailable",
			input:        status.Error(codes.Unavailable, "server offline"),
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "gRPC DeadlineExceeded",
			input:        status.Error(codes.DeadlineExceeded, "timeout"),
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "gRPC Aborted",
			input:        status.Error(codes.Aborted, "conflict"),
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "gRPC Canceled",
			input:        status.Error(codes.Canceled, "operation cancelled"),
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "gRPC InvalidArgument",
			input:        status.Error(codes.InvalidArgument, "bad param"),
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "gRPC PermissionDenied",
			input:        status.Error(codes.PermissionDenied, "no access"),
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:          "String matching UNAUTHENTICATED",
			input:         errors.New("Error: UNAUTHENTICATED"),
			expectedWrap:  llm.ErrAuth,
			containsMatch: "UNAUTHENTICATED",
		},
		{
			name:          "String matching API_KEY_INVALID",
			input:         errors.New("some error: API_KEY_INVALID"),
			expectedWrap:  llm.ErrAuth,
			containsMatch: "API_KEY_INVALID",
		},
		{
			name:          "String matching RESOURCE_EXHAUSTED",
			input:         errors.New("RESOURCE_EXHAUSTED"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "RESOURCE_EXHAUSTED",
		},
		{
			name:          "String matching QUOTA",
			input:         errors.New("exceeded QUOTA"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "QUOTA",
		},
		{
			name:          "String matching 429",
			input:         errors.New("got HTTP 429 error"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "429",
		},
		{
			name:          "User reported Error 504",
			input:         errors.New("Error 504, Message: Deadline expired before operation could complete., Status: DEADLINE_EXCEEDED, Details: []"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "504",
		},
		{
			name:          "String matching Error 504 (Task 1 Regression)",
			input:         errors.New("Error 504, Message: The server encountered a temporary error and could not complete your request., Status: , Details: []"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "504",
		},
		{
			name:          "String matching 504 without prefix",
			input:         errors.New("something went wrong: 504"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "504",
		},
		{
			name:          "String matching Gateway Timeout with colon",
			input:         errors.New("Gateway: Timeout"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "TIMEOUT",
		},
		{
			name:          "String matching 499 and CANCELLED",
			input:         errors.New("Error 499, Message: The operation was cancelled., Status: CANCELLED"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "CANCELLED",
		},
		{
			name:          "String matching UNAVAILABLE",
			input:         errors.New("Status: UNAVAILABLE"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "UNAVAILABLE",
		},
		{
			name:          "String matching HTTP 408",
			input:         errors.New("Request Timeout (Status Code: 408)"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "408",
		},
		{
			name:          "String matching 5xx range (507)",
			input:         errors.New("Internal Server Error (507)"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "507",
		},
		{
			name:          "String matching 429 without prefix",
			input:         errors.New("something went wrong: 429"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "429",
		},
		{
			name:          "String matching Error 429 (Task 4 Regression)",
			input:         errors.New("Error 429: Rate limit reached"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "429",
		},
		{
			name:          "String matching RATE LIMIT with space",
			input:         errors.New("exceeded RATE LIMIT"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "RATE LIMIT",
		},
		{
			name:          "String matching CONNECTION RESET BY PEER fallback",
			input:         errors.New("read tcp 127.0.0.1: read: connection reset by peer"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "CONNECTION RESET BY PEER",
		},
		{
			name:          "String matching BROKEN PIPE fallback",
			input:         errors.New("write tcp 127.0.0.1: write: broken pipe"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "BROKEN PIPE",
		},
		{
			name:          "String matching EOF fallback",
			input:         errors.New("unexpected EOF"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "EOF",
		},
		{
			name:          "HTTP/2 stream INTERNAL_ERR",
			input:         errors.New("stream error: stream ID 7; INTERNAL_ERR"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "INTERNAL_ERR",
		},
		{
			name:          "HTTP/2 stream error generic",
			input:         errors.New("stream error: stream ID 3; PROTOCOL_ERROR"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "PROTOCOL_ERROR",
		},
		{
			name:         "wrapped syscall.ECONNRESET",
			input:        fmt.Errorf("read tcp 10.80.9.9:47548->3.173.21.63:443: read: %w", syscall.ECONNRESET),
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "syscall.ECONNRESET",
			input:        syscall.ECONNRESET,
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "syscall.ECONNREFUSED",
			input:        syscall.ECONNREFUSED,
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "context.DeadlineExceeded",
			input:        context.DeadlineExceeded,
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "context.Canceled",
			input:        context.Canceled,
			expectedWrap: context.Canceled, // not transient; cancellation is terminal
		},
		{
			name:         "net.Error with Timeout",
			input:        &mockNetError{msg: "i/o timeout", timeout: true},
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "HTTPStatusErr with non-classifiable status 200",
			input:        &mockHTTPStatusErr{status: 200},
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "HTTPStatusErr with zero status",
			input:        &mockHTTPStatusErr{status: 0},
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "Unclassified error defaults to terminal",
			input:        errors.New("unknown error"),
			expectedWrap: llm.ErrTerminal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.input)

			if tt.input == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}

			if !errors.Is(got, tt.expectedWrap) {
				t.Errorf("expected error to wrap %v, got %v", tt.expectedWrap, got)
			}

			if tt.containsMatch != "" {
				if !strings.Contains(strings.ToUpper(got.Error()), tt.containsMatch) {
					t.Errorf("expected error message to contain %q, got %q", tt.containsMatch, got.Error())
				}
			}
		})
	}
}

func TestClassify_MultiWrap(t *testing.T) {
	originalErr := &APIError{Status: 429, Body: "Too Many Requests"}
	classifiedErr := Classify(originalErr)

	// Verify we can find the domain sentinel
	if !errors.Is(classifiedErr, llm.ErrRateLimit) {
		t.Errorf("expected classified error to be llm.ErrRateLimit")
	}

	// Verify we can find the original error (this requires %w for the second arg)
	if !errors.Is(classifiedErr, originalErr) {
		t.Errorf("expected classified error to also wrap the original error")
	}

	// Verify we can extract the original error via errors.As
	var apiErr *APIError
	if !errors.As(classifiedErr, &apiErr) {
		t.Errorf("expected classified error to be extractable as *APIError")
	} else if apiErr.Status != 429 {
		t.Errorf("extracted APIError has wrong status: got %d, want 429", apiErr.Status)
	}
}

func TestClassify_GreedyAvoidance(t *testing.T) {
	tests := []struct {
		name         string
		input        error
		expectedWrap error
	}{
		{
			name:         "Token limit 500 should be terminal",
			input:        errors.New("prompt exceeds 500 tokens"),
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "Internal ID should be terminal",
			input:        errors.New("invalid field: internal_id"),
			expectedWrap: llm.ErrTerminal,
		},
		{
			name:         "Timeout in text should be terminal if not a system timeout",
			input:        errors.New("the user mentioned a timeout in their prompt"),
			expectedWrap: llm.ErrTerminal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.input)
			if !errors.Is(got, tt.expectedWrap) {
				t.Errorf("Classify(%q) = %v; want %v", tt.input.Error(), got, tt.expectedWrap)
			}
		})
	}
}

// mockNetError implements net.Error for testing classifyStandard.
type mockNetError struct {
	msg     string
	timeout bool
}

func (e *mockNetError) Error() string   { return e.msg }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return false }

// mockHTTPStatusErr implements HTTPStatusErr for testing classifyHTTP fallthrough.
type mockHTTPStatusErr struct {
	status int
}

func (e *mockHTTPStatusErr) Error() string   { return fmt.Sprintf("http %d", e.status) }
func (e *mockHTTPStatusErr) StatusCode() int { return e.status }

func TestIsTerminalQuotaError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "Kimi exceeded_current_quota_error",
			body: `{"error": {"type": "exceeded_current_quota_error", "message": "Account balance is insufficient"}}`,
			want: true,
		},
		{
			name: "OpenAI insufficient_quota",
			body: `{"error": {"type": "insufficient_quota", "message": "You exceeded your current quota"}}`,
			want: true,
		},
		{
			name: "Anthropic rate_limit_error",
			body: `{"error": {"type": "rate_limit_error", "message": "..."}}`,
			want: false,
		},
		{
			name: "Kimi engine_overloaded_error",
			body: `{"error": {"type": "engine_overloaded_error", "message": "..."}}`,
			want: false,
		},
		{
			name: "empty body",
			body: "",
			want: false,
		},
		{
			name: "not json",
			body: "not json",
			want: false,
		},
		{
			name: "no type field",
			body: `{"error": {}}`,
			want: false,
		},
		{
			name: "no message field",
			body: `{"error": {"type": "exceeded_current_quota_error"}}`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTerminalQuotaError(tt.body)
			if got != tt.want {
				t.Errorf("isTerminalQuotaError(%q) = %v; want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestClassify_QuotaExhausted(t *testing.T) {
	err := &APIError{
		Status: 429,
		Body:   `{"error": {"type": "exceeded_current_quota_error", "message": "Account balance is insufficient"}}`,
	}
	result := Classify(err)
	if !errors.Is(result, llm.ErrQuotaExhausted) {
		t.Errorf("expected llm.ErrQuotaExhausted, got %v", result)
	}
	// Quota exhaustion must NOT be transient — same-provider retry should stop.
	if llm.IsTransient(result) {
		t.Error("ErrQuotaExhausted should not be transient (must stop same-provider retry)")
	}
}

func TestClassify_RetryableRateLimit_Unaffected(t *testing.T) {
	err := &APIError{
		Status: 429,
		Body:   `{"error": {"type": "rate_limit_error", "message": "..."}}`,
	}
	result := Classify(err)
	if !errors.Is(result, llm.ErrRateLimit) {
		t.Errorf("expected llm.ErrRateLimit, got %v", result)
	}
}

func TestIsContentFilterError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "Kimi content_filter",
			body: `{"error": {"type": "content_filter", "message": "high risk"}}`,
			want: "high risk",
		},
		{
			name: "invalid_request_error",
			body: `{"error": {"type": "invalid_request_error", "message": "bad"}}`,
			want: "",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
		{
			name: "not json",
			body: "not json",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isContentFilterError(tt.body)
			if got != tt.want {
				t.Errorf("isContentFilterError(%q) = %q; want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestClassify_ContentFilter(t *testing.T) {
	err := &APIError{
		Status: 400,
		Body:   `{"error": {"type": "content_filter", "message": "high risk"}}`,
	}
	result := Classify(err)
	if !errors.Is(result, llm.ErrContentFilter) {
		t.Errorf("expected llm.ErrContentFilter, got %v", result)
	}
}

func TestClassify_ContentFilter_Idempotent(t *testing.T) {
	// First pass: classify the raw APIError
	apiErr := &APIError{
		Status: 400,
		Body:   `{"error": {"type": "content_filter", "message": "high risk"}}`,
	}
	result1 := Classify(apiErr)
	if !errors.Is(result1, llm.ErrContentFilter) {
		t.Fatalf("first pass: expected ErrContentFilter, got %v", result1)
	}
	// Second pass: classify the already-classified error
	// Must be idempotent — should not re-wrap or change the sentinel
	result2 := Classify(result1)
	if !errors.Is(result2, llm.ErrContentFilter) {
		t.Errorf("second pass: expected ErrContentFilter, got %v (not idempotent)", result2)
	}
}
