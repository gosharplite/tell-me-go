// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmerr

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

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
	reTransient = regexp.MustCompile(`(?i)(\b(?:HTTP|STATUS|CODE|ERROR|ERR|STATUS_CODE)[\s_:]*(?:5\d{2}|499|408)\b|[:]\s*(?:5\d{2}|499|408)\b|CANCELLED|INTERNAL[\s_:]+SERVER[\s_:]+ERROR|BAD[\s_:]+GATEWAY|SERVICE[\s_:]+UNAVAILABLE|DEADLINE[\s_:]+EXCEEDED|UNAVAILABLE|\b(?:CONNECTION|REQUEST|GATEWAY|OPERATION)[\s_:]+TIMEOUT\b|\bCONNECTION[\s_:]+RESET\b)`)
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

	if wrapped, ok := classifyString(err); ok {
		return wrapped
	}

	// 4. Default to terminal if we can't classify it
	return fmt.Errorf("%w: %w", llm.ErrTerminal, err)
}

func classifyDomain(err error) (error, bool) {
	if errors.Is(err, llm.ErrAuth) || errors.Is(err, llm.ErrTransient) || errors.Is(err, llm.ErrTerminal) || errors.Is(err, llm.ErrRateLimit) {
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

func classifyHTTP(err error) (error, bool) {
	var httpErr HTTPStatusErr
	if errors.As(err, &httpErr) {
		status := httpErr.StatusCode()
		switch {
		case status == 401:
			return fmt.Errorf("%w: %w", llm.ErrAuth, err), true
		case status == 429:
			return fmt.Errorf("%w: %w", llm.ErrRateLimit, err), true
		case status == 499 || status == 408:
			return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
		case status >= 500:
			return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: %w", llm.ErrTerminal, err), true
		}
	}
	return nil, false
}

func classifyString(err error) (error, bool) {
	msg := err.Error() // Regex handles (?i) case-insensitivity

	if strings.Contains(strings.ToUpper(msg), "UNAUTHENTICATED") || strings.Contains(strings.ToUpper(msg), "API_KEY_INVALID") {
		return fmt.Errorf("%w: %w", llm.ErrAuth, err), true
	}

	if reRateLimit.MatchString(msg) {
		return fmt.Errorf("%w: %w", llm.ErrRateLimit, err), true
	}

	if reTransient.MatchString(msg) {
		return fmt.Errorf("%w: %w", llm.ErrTransient, err), true
	}

	return nil, false
}
