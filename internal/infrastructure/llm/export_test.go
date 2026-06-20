// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import "net/http"

// GetHTTPClient exposes the private httpClient field for tests.
var GetHTTPClient = func(c *llmProviderHealthChecker) *http.Client {
	return c.httpClient
}
