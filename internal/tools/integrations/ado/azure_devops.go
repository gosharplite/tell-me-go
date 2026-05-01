// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type securityConfirmer interface {
	Confirm(ctx context.Context, message string) (bool, error)
}

type adoManager struct {
	sc                 securityConfirmer
	httpClient         tools.HTTPClient
	token              string
	authHeader         string
	baseURL            string // For testing
	pipelineCache      sync.Map
	pipelineFetchGroup singleflight.Group
}

// AdoOption is a functional option for configuring the adoManager.
type AdoOption func(*adoManager)

// withBaseURL sets the base URL for Azure DevOps API requests.
func withBaseURL(url string) AdoOption {
	return func(m *adoManager) {
		m.baseURL = url
	}
}

// WithHTTPClient sets the HTTP client for Azure DevOps API requests.
func WithHTTPClient(client tools.HTTPClient) AdoOption {
	return func(m *adoManager) {
		m.httpClient = client
	}
}

// WithToken sets the Azure DevOps Personal Access Token (PAT).
func WithToken(token string) AdoOption {
	return func(m *adoManager) {
		m.token = token
		if token != "" {
			auth := ":" + token
			m.authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
		}
	}
}

// NewADOManager creates a new instance of adoManager.
func NewADOManager(sc securityConfirmer, opts ...AdoOption) *adoManager {
	m := &adoManager{
		sc:         sc,
		baseURL:    "https://dev.azure.com",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *adoManager) getAuthHeader() (string, error) {
	if m.authHeader == "" {
		return "", fmt.Errorf("AZURE_PAT_ALL token is required but not provided")
	}
	return m.authHeader, nil
}

func (m *adoManager) executeRequest(ctx context.Context, method, requestURL string, body io.Reader, headers map[string]string) (*http.Response, error) {
	authHeader, err := m.getAuthHeader()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)

	hasAccept := false
	for k, v := range headers {
		req.Header.Set(k, v)
		if strings.EqualFold(k, "Accept") {
			hasAccept = true
		}
	}

	if !hasAccept {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if err := m.checkResponseError(resp, requestURL); err != nil {
		return nil, err
	}

	return resp, nil
}

func (m *adoManager) checkResponseError(resp *http.Response, requestURL string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("azure DevOps API returned status %s; additionally, failed to read response body: %w", resp.Status, err)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
	case http.StatusForbidden:
		return fmt.Errorf("forbidden: you don't have permission to access this resource")
	case http.StatusNotFound:
		return fmt.Errorf("resource not found (404): %s", requestURL)
	default:
		return fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(respBody))
	}
}

func (m *adoManager) AdoGetFileContent(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		Path         string `json:"path"`
		Version      string `json:"version"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get file content args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.Path == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and path are required")
	}

	if params.Version == "" {
		params.Version = "main"
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository)))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("path", params.Path)
	q.Set("versionDescriptor.version", params.Version)
	q.Set("$format", "text")
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get file content request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	return tools.ToolResult{Text: string(content)}, nil
}

type adoRepositoryItemsResponse struct {
	Value []struct {
		Path     string `json:"path"`
		IsFolder bool   `json:"isFolder"`
	} `json:"value"`
	Count int `json:"count"`
}

func (m *adoManager) AdoListRepositoryItems(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Organization   string `json:"organization"`
		Project        string `json:"project"`
		Repository     string `json:"repository"`
		ScopePath      string `json:"scope_path"`
		Version        string `json:"version"`
		RecursionLevel string `json:"recursion_level"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing list repository items args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and repository are required")
	}

	requestURL, err := m.buildListRepositoryItemsURL(params.Organization, params.Project, params.Repository, params.ScopePath, params.Version, params.RecursionLevel)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("building list repository items URL: %w", err)
	}

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing list repository items request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var responseData adoRepositoryItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return m.formatRepositoryItems(params.ScopePath, params.Version, responseData), nil
}

func (m *adoManager) buildListRepositoryItemsURL(org, project, repo, scopePath, version, recursionLevel string) (string, error) {
	if scopePath == "" {
		scopePath = "/"
	}
	if version == "" {
		version = "main"
	}
	if recursionLevel == "" {
		recursionLevel = "none"
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items",
		m.baseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo)))
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("scopePath", scopePath)
	q.Set("versionDescriptor.version", version)
	q.Set("recursionLevel", recursionLevel)
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (m *adoManager) formatRepositoryItems(scopePath, version string, responseData adoRepositoryItemsResponse) tools.ToolResult {
	if len(responseData.Value) == 0 {
		return tools.ToolResult{Text: "No items found."}
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Items in %s (%s):\n\n", scopePath, version)
	for _, item := range responseData.Value {
		// Skip the scope path itself if it's the first element
		if item.Path == scopePath && len(responseData.Value) > 1 {
			continue
		}
		prefix := "[FILE]"
		if item.IsFolder {
			prefix = "[DIR] "
		}
		_, _ = fmt.Fprintf(&resultText, "- %s %s\n", prefix, item.Path)
	}

	return tools.ToolResult{Text: resultText.String()}
}

// Helper functions to reduce complexity in formatKey
func isLower(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func toUpper(r rune) rune {
	if isLower(r) {
		return r - 'a' + 'A'
	}
	return r
}

func formatKey(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var res strings.Builder

	for i, r := range runes {
		if i == 0 {
			res.WriteRune(toUpper(r))
			continue
		}

		if isUpper(r) {
			// Add space if preceded by lowercase OR followed by lowercase (e.g., HTMLReader -> HTML Reader)
			prevLower := isLower(runes[i-1])
			nextLower := i+1 < len(runes) && isLower(runes[i+1])
			if prevLower || nextLower {
				res.WriteRune(' ')
			}
		}
		res.WriteRune(r)
	}

	result := res.String()
	if strings.HasSuffix(result, " Id") {
		result = result[:len(result)-2] + "ID"
	}
	return result
}
