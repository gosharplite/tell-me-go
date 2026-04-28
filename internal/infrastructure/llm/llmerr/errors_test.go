package llmerr

import (
	"errors"
	"strings"
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
			name:          "connection reset by peer",
			input:         errors.New("read tcp 10.80.9.9:47548->3.173.21.63:443: read: connection reset by peer"),
			expectedWrap:  llm.ErrTransient,
			containsMatch: "TRANSIENT",
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
