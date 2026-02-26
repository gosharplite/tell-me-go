package llmerr

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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
			name:         "HTTP 503 via APIError",
			input:        &APIError{Status: 503, Body: "Service Unavailable"},
			expectedWrap: llm.ErrTransient,
		},
		{
			name:         "HTTP 400 via APIError",
			input:        &APIError{Status: 400, Body: "Bad Request"},
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
			input:         errors.New("got 429 error"),
			expectedWrap:  llm.ErrRateLimit,
			containsMatch: "429",
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
