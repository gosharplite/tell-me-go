// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmerr

import (
	"fmt"
)

// APIError represents an error returned by an LLM provider's API.
// It implements the httpStatusErr interface used by ResilientClient.
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
