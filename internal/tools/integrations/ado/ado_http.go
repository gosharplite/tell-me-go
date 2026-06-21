// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// executeAdoGet executes an HTTP GET request against the Azure DevOps API and
// decodes the JSON response body into a value of type T.
//
// The caller is responsible for ensuring T matches the expected API response
// schema. On HTTP errors (non-2xx), the error is returned without attempting
// JSON decode. On decode failures, the response body close is handled by the
// deferred call — double-close is safe per http.Response.Body.Close contract.
//
// Headers are optional; when nil, ExecuteRequest applies its default
// Accept: application/json. Pass a non-nil map to override (e.g.,
// {"Accept": "*/*"} for endpoints that require a non-standard media type but
// still return JSON).
func executeAdoGet[T any](ctx context.Context, m *AdoManager, url string, headers map[string]string) (T, error) {
	var result T
	resp, err := m.ExecuteRequest(ctx, http.MethodGet, url, nil, headers)
	if err != nil {
		return result, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}
