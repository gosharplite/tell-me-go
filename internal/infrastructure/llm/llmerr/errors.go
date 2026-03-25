// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmerr

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

var (
	reRateLimit = regexp.MustCompile(`(?i)(HTTP 429|STATUS: 429|RATE_LIMIT_EXCEEDED|RESOURCE_EXHAUSTED|QUOTA|RESOURCE EXHAUSTED)`)
	reTransient = regexp.MustCompile(`(?i)(HTTP 50[0234]|STATUS: 50[0234]|INTERNAL_SERVER_ERROR|INTERNAL SERVER ERROR|BAD_GATEWAY|BAD GATEWAY|SERVICE_UNAVAILABLE|SERVICE UNAVAILABLE|GATEWAY_TIMEOUT|GATEWAY TIMEOUT|DEADLINE_EXCEEDED|UNAVAILABLE|(?:CONNECTION|REQUEST|GATEWAY|OPERATION)[_ ]TIMEOUT)`)
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

func classifyHTTP(err error) (error, bool) {
	var httpErr HTTPStatusErr
	if errors.As(err, &httpErr) {
		status := httpErr.StatusCode()
		switch {
		case status == 401:
			return fmt.Errorf("%w: %w", llm.ErrAuth, err), true
		case status == 429:
			return fmt.Errorf("%w: %w", llm.ErrRateLimit, err), true
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
