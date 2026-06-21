// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/base64"
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

type AdoManager struct {
	sc                 securityConfirmer
	httpClient         tools.HTTPClient
	token              string
	authHeader         string
	BaseURL            string // For testing
	pipelineCache      sync.Map
	pipelineFetchGroup singleflight.Group
}

// AdoOption is a functional option for configuring the AdoManager.
type AdoOption func(*AdoManager)

// WithBaseURL sets the base URL for Azure DevOps API requests.
func WithBaseURL(url string) AdoOption {
	return func(m *AdoManager) {
		m.BaseURL = url
	}
}

// WithHTTPClient sets the HTTP client for Azure DevOps API requests.
func WithHTTPClient(client tools.HTTPClient) AdoOption {
	return func(m *AdoManager) {
		m.httpClient = client
	}
}

// WithToken sets the Azure DevOps Personal Access Token (PAT).
func WithToken(token string) AdoOption {
	return func(m *AdoManager) {
		m.token = token
		if token != "" {
			auth := ":" + token
			m.authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
		}
	}
}

// NewADOManager creates a new instance of AdoManager.
func NewADOManager(sc securityConfirmer, opts ...AdoOption) *AdoManager {
	m := &AdoManager{
		sc:         sc,
		BaseURL:    "https://dev.azure.com",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *AdoManager) getAuthHeader() (string, error) {
	if m.authHeader == "" {
		return "", fmt.Errorf("AZURE_PAT_ALL token is required but not provided")
	}
	return m.authHeader, nil
}

func (m *AdoManager) ExecuteRequest(ctx context.Context, method, requestURL string, body io.Reader, headers map[string]string) (*http.Response, error) {
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

	if err := m.CheckResponseError(resp, requestURL); err != nil {
		return nil, err
	}

	return resp, nil
}

func (m *AdoManager) CheckResponseError(resp *http.Response, requestURL string) error {
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

func (m *AdoManager) adoGetFileContent(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	if err := validateGetFileContentParams(params.Organization, params.Project, params.Repository, params.Path); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Version == "" {
		params.Version = "main"
	}

	requestURL, err := m.buildGetFileContentURL(params.Organization, params.Project, params.Repository, params.Path, params.Version)
	if err != nil {
		return tools.ToolResult{}, err
	}

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, map[string]string{"Accept": "*/*"})
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

func validateGetFileContentParams(org, project, repo, path string) error {
	if org == "" || project == "" || repo == "" || path == "" {
		return fmt.Errorf("organization, project, repository, and path are required")
	}
	return nil
}

func (m *AdoManager) buildGetFileContentURL(org, project, repo, path, version string) (string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo)))
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}
	q := u.Query()
	q.Set("path", path)
	q.Set("versionDescriptor.version", version)
	q.Set("$format", "text")
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type adoRepositoryItemsResponse struct {
	Value []struct {
		Path     string `json:"path"`
		IsFolder bool   `json:"isFolder"`
	} `json:"value"`
	Count int `json:"count"`
}

func (m *AdoManager) AdoListRepositoryItems(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	responseData, err := executeAdoGet[adoRepositoryItemsResponse](ctx, m, requestURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("listing repository items: %w", err)
	}

	return formatRepositoryItems(params.ScopePath, params.Version, responseData), nil
}

func (m *AdoManager) buildListRepositoryItemsURL(org, project, repo, scopePath, version, recursionLevel string) (string, error) {
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
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo)))
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
