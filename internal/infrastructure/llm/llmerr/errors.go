// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmerr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"syscall"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// reRateLimit identifies standard rate limiting signals from providers.
	reRateLimit = regexp.MustCompile(`(?i)(\b(?:HTTP|STATUS|CODE|ERROR|ERR|STATUS_CODE)[\s_:]*(?:429)\b|[:]\s*(?:429)\b|RATE[\s_:]*LIMIT|RESOURCE[\s_:]*EXHAUSTED|QUOTA)`)

	// reTransient matches infrastructure errors that should trigger an automatic retry.
	// Design: Permissive but non-greedy.
	// 1. Match 5xx/499/408 codes with various prefixes (HTTP, Status, Error, Code, etc) or common delimiters (:).
	// 2. Uses word boundaries (\b) and specific prefix requirements to avoid greedy false positives
	//    like "500 tokens" or "context window: 128000".
	// 3. Delimiters are flexible (space, underscore, colon) to handle varied SDK logging formats.
	reTransient = regexp.MustCompile(`(?i)(\b(?:HTTP|STATUS|CODE|ERROR|ERR|STATUS_CODE)[\s_:]*(?:5\d{2}|499|408)\b|[:]\s*(?:5\d{2}|499|408)\b|CANCELLED|INTERNAL[\s_:]+SERVER[\s_:]+ERROR|INTERNAL_ERR|STREAM[\s_]+ERROR|BAD[\s_:]+GATEWAY|SERVICE[\s_:]+UNAVAILABLE|DEADLINE[\s_:]+EXCEEDED|UNAVAILABLE|\b(?:CONNECTION|REQUEST|GATEWAY|OPERATION)[\s_:]+TIMEOUT\b|(?:\bCONNECTION\s+RESET\s+BY\s+PEER\b)|(?:\bBROKEN\s+PIPE\b)|(?:\bEOF\b))`)

	// reAuth identifies authentication and authorization failures in opaque error strings.
	reAuth = regexp.MustCompile(`(?i)\b(UNAUTHENTICATED|API_KEY_INVALID)\b`)
)

// APIError represents an error returned by an LLM provider's API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error (status %d): %s", e.Status, e.Body)
}

func (e *APIError) StatusCode() int {
	return e.Status
}

// HTTPStatusErr captures various HTTP error implementations in SDKs.
type HTTPStatusErr interface {
	StatusCode() int
}

// Classify converts a raw error (including APIError) into a domain-specific error.
func Classify(err error) error {
	if err == nil {
		return nil
	}

	if wrapped, ok := classifyDomain(err); ok {
		return wrapped
	}

	if wrapped, ok := classifyGRPC(err); ok {
		return wrapped
	}

	if wrapped, ok := classifyHTTP(err); ok {
		return wrapped
	}

	if wrapped, ok := classifyStandard(err); ok {
		return wrapped
	}

	if wrapped, ok := classifyString(err); ok {
		return wrapped
	}

	// 4. Default to terminal if we can't classify it
	return fmt.Errorf("%w: %w", llm.ErrTerminal, err)
}

func classifyDomain(err error) (error, bool) {
	if errors.Is(err, llm.ErrAuth) || errors.Is(err, llm.ErrTransient) || errors.Is(err, llm.ErrTerminal) || errors.Is(err, llm.ErrRateLimit) || errors.Is(err, llm.ErrQuotaExhausted) || errors.Is(err, llm.ErrContentFilter) {
		return err, true
	}
	return nil, false
}

func classifyGRPC(err error) (error, bool) {
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unauthenticated:
			return fmt.Errorf("%w: %w", llm.ErrAuth, err), true
		case codes.ResourceExhausted:
			// gRPC ResourceExhausted is often a rate limit in LLM providers
			return fmt.Errorf("%w: %w", llm.ErrRateLimit, err), true
		case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.Canceled:
			return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
		case codes.PermissionDenied, codes.InvalidArgument:
			return fmt.Errorf("%w: %w", llm.ErrTerminal, err), true
		}
	}
	return nil, false
}

// httpStatusToDomain maps HTTP status codes to domain error sentinels.
// It is extracted from classifyHTTP to keep both functions below the
// cyclomatic complexity threshold (≤10).
func httpStatusToDomain(status int) error {
	switch {
	case status == 401:
		return llm.ErrAuth
	case status == 402:
		return llm.ErrQuotaExhausted
	case status == 429:
		return llm.ErrRateLimit
	case status == 499 || status == 408:
		return llm.ErrTransient
	case status >= 500:
		return llm.ErrTransient
	case status >= 400 && status < 500:
		return llm.ErrTerminal
	}
	return nil
}

// providerError holds the parsed fields from a provider JSON error body
// of the form {"error": {"type": "...", "message": "..."}}.
type providerError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseProviderError attempts to parse a provider JSON error body.
// Returns the parsed fields and true on success, or zero values and false
// on any failure (empty body, invalid JSON).
func parseProviderError(body string) (typ, msg string, ok bool) {
	if body == "" {
		return "", "", false
	}
	var pe providerError
	if err := json.Unmarshal([]byte(body), &pe); err != nil {
		return "", "", false
	}
	return pe.Error.Type, pe.Error.Message, true
}

// isTerminalQuotaError parses a provider error body to detect quota-exhaustion
// errors that should not be retried. Returns true when the error.type field
// indicates a terminal billing/quota failure rather than a transient rate limit.
//
// Recognised types:
//   - "exceeded_current_quota_error" (Kimi)
//   - "insufficient_quota"           (OpenAI, DeepSeek)
//
// The function is defensive: any parse failure or unrecognised type
// returns false, preserving the existing retry behaviour.
func isTerminalQuotaError(body string) bool {
	typ, _, ok := parseProviderError(body)
	if !ok {
		return false
	}
	return typ == "exceeded_current_quota_error" || typ == "insufficient_quota"
}

// isContentFilterError parses a provider error body to detect content
// safety rejections. Returns the provider's rejection message when the
// error.type field is "content_filter", or empty string otherwise.
//
// The function is defensive: any parse failure or unrecognised type
// returns an empty string.
func isContentFilterError(body string) string {
	typ, msg, ok := parseProviderError(body)
	if !ok || typ != "content_filter" {
		return ""
	}
	return msg
}

func classifyHTTP(err error) (error, bool) {
	var httpErr HTTPStatusErr
	if !errors.As(err, &httpErr) {
		return nil, false
	}
	domainErr := httpStatusToDomain(httpErr.StatusCode())
	if domainErr == nil {
		return nil, false
	}

	// Single cast to *APIError — used by both override blocks below.
	var apiErr *APIError
	_ = errors.As(err, &apiErr)

	// Override: if the status code maps to rate limit but the error body
	// indicates a terminal quota exhaustion (e.g. empty account balance),
	// classify as quota exhausted — stops same-provider retry but allows
	// cross-provider failover.
	if domainErr == llm.ErrRateLimit {
		if apiErr != nil && isTerminalQuotaError(apiErr.Body) {
			return fmt.Errorf("%w: %w", llm.ErrQuotaExhausted, err), true
		}
	}
	// Override: if the status code maps to terminal but the error body
	// indicates a content safety rejection, classify as content filter
	// so the operator can see the rejection reason and adjust the input
	// before retrying. (Future: feed back into the model loop for in-session
	// self-correction.)
	if domainErr == llm.ErrTerminal {
		if apiErr != nil {
			if msg := isContentFilterError(apiErr.Body); msg != "" {
				return fmt.Errorf("%w: %s: %w", llm.ErrContentFilter, msg, err), true
			}
		}
	}
	return fmt.Errorf("%w: %w", domainErr, err), true
}

// classifyStandard intercepts standard Go network and context errors
// using errors.Is / errors.As rather than brittle string matching.
func classifyStandard(err error) (error, bool) {
	// 1. TCP-level connection failures
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
	}

	// 2. Context deadline (transient: fresh context may succeed on retry)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
	}

	// 3. Context cancellation (terminal: caller explicitly abandoned the operation)
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("operation canceled: %w", err), true
	}

	// 4. Generic Go network timeouts (net.Error interface)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
	}

	return nil, false
}

func classifyString(err error) (error, bool) {
	msg := err.Error() // Regex handles (?i) case-insensitivity

	// Auth must be checked first: auth failures are non-retryable
	if reAuth.MatchString(msg) {
		return fmt.Errorf("%w: %w", llm.ErrAuth, err), true
	}

	if reRateLimit.MatchString(msg) {
		return fmt.Errorf("%w: %w", llm.ErrRateLimit, err), true
	}

	// Transient is the broadest category; evaluate last to let more specific
	// classifiers (auth, rate-limit) take priority.
	if reTransient.MatchString(msg) {
		return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
	}

	return nil, false
}
