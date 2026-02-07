// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import "net/http"

// HTTPClient defines the interface for making HTTP requests.
// It is compatible with *http.Client.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
