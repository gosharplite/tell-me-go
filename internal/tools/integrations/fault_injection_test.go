// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSecurityManager provides a test double for security.Manager used in ADO/Atlassian tests.
type mockSecurityManager struct {
	approved      bool
	err           error
	confirmCalled bool
}

func (m *mockSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (m *mockSecurityManager) IsPathWritable(path string) (string, error) {
	return path, nil
}
func (m *mockSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return m.approved, m.err
}
func (m *mockSecurityManager) LogAudit(action string, args ...any) {}
func (m *mockSecurityManager) TerminalLock()                       {}
func (m *mockSecurityManager) TerminalUnlock()                     {}
func (m *mockSecurityManager) Prompt(message string)               {}
func (m *mockSecurityManager) Warn(message string)                 {}
func (m *mockSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	m.confirmCalled = true
	return m.approved, m.err
}
func (m *mockSecurityManager) ReadLine(ctx context.Context) (string, error) { return "", nil }
func (m *mockSecurityManager) IsCommandAllowed(command string) bool         { return true }
func (m *mockSecurityManager) IsBypassActive() bool                         { return false }
func (m *mockSecurityManager) Close() error                                 { return nil }

type mockRoundTripper struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

func TestADOManager_ErrorPaths(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		inputURL   string
		call       func(ctx context.Context, m *ado.AdoManager) error
		wantErrMsg string
	}{
		{
			name: "Request Creation Failure",
			// Invalid URL with control character to force http.NewRequestWithContext to fail
			inputURL: "https://api.example.com/" + string([]byte{0x7f}),
			call: func(ctx context.Context, m *ado.AdoManager) error {
				_, err := m.ExecuteRequest(ctx, http.MethodGet, m.BaseURL, nil, nil)
				return err
			},
			wantErrMsg: "failed to create request",
		},
		{
			name: "Network RoundTrip Failure",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return nil, errors.New("network unreachable")
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				_, err := m.ExecuteRequest(ctx, http.MethodGet, "https://api.example.com", nil, nil)
				return err
			},
			wantErrMsg: "network unreachable",
		},
		{
			name: "Malformed JSON Response in adoListRepositoryItems",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{bad-json}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"repository":   "repo",
				}
				_, err := m.AdoListRepositoryItems(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Context Cancellation in executeRequest",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return nil, context.Canceled
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				_, err := m.ExecuteRequest(ctx, http.MethodGet, "https://api.example.com", nil, nil)
				return err
			},
			wantErrMsg: "context canceled",
		},
		{
			name: "Unmarshal Failure in adoGetPipelineRun",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"id": "not-an-int"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"pipeline_id":  1,
					"run_id":       1,
				}
				_, err := m.AdoGetPipelineRun(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			baseURL := "https://dev.azure.com"
			if tt.inputURL != "" {
				baseURL = tt.inputURL
			}

			m := ado.NewADOManager(sm,
				ado.WithBaseURL(baseURL),
				ado.WithHTTPClient(client),
				ado.WithToken("test-pat"),
			)

			err := tt.call(context.Background(), m)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestAtlassianProvider_ErrorPaths(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://jira.com")
	p, err := atlassian.NewAtlassianProvider()
	require.NoError(t, err)
	p.Email = "test@example.com"
	p.Token = "test-token"

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		call       func(ctx context.Context, client tools.HTTPClient) error
		wantErrMsg string
	}{
		{
			name: "Network Failure",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return nil, errors.New("connection reset")
				}
			},
			call: func(ctx context.Context, client tools.HTTPClient) error {
				req, _ := http.NewRequest(http.MethodGet, "https://jira.com", nil)
				_, err := p.Do(ctx, client, req)
				return err
			},
			wantErrMsg: "connection reset",
		},
		{
			name: "Retry Body Reset Failure",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Header:     http.Header{"Retry-After": []string{"0"}},
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}
			},
			call: func(ctx context.Context, client tools.HTTPClient) error {
				req, _ := http.NewRequest(http.MethodPost, "https://jira.com", strings.NewReader("body"))
				// Force GetBody to return an error
				req.GetBody = func() (io.ReadCloser, error) {
					return nil, errors.New("body reset failed")
				}
				_, err := p.Do(ctx, client, req)
				return err
			},
			wantErrMsg: "failed to reset request body: body reset failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			err := tt.call(context.Background(), client)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestConfluenceManager_ErrorPaths(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		call       func(ctx context.Context, m *atlassian.ConfluenceManager) error
		wantErrMsg string
	}{
		{
			name: "JSON Decode Failure in fetchSearchPage",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{invalid}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *atlassian.ConfluenceManager) error {
				_, err := m.FetchSearchPage(ctx, "https://confluence.com/api")
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in decodePageResponse",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"title": 123}`)), // title should be string
					}, nil
				}
			},
			call: func(ctx context.Context, m *atlassian.ConfluenceManager) error {
				args := map[string]interface{}{"page_id": "123"}
				_, err := m.ConfluenceRead(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
			t.Setenv("ATLASSIAN_TOKEN", "test-token")
			t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			m, err := atlassian.NewConfluenceManager(sm, client)
			require.NoError(t, err)

			err = tt.call(context.Background(), m)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestJiraManager_ErrorPaths(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		call       func(ctx context.Context, m *atlassian.JiraManager) error
		wantErrMsg string
	}{
		{
			name: "JSON Decode Failure in jiraSearchIssues",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"issues": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *atlassian.JiraManager) error {
				args := map[string]interface{}{"jql": "project=PROJ"}
				_, err := m.JiraSearchIssues(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in jiraGetIssue",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"key": 123}`)), // key should be string
					}, nil
				}
			},
			call: func(ctx context.Context, m *atlassian.JiraManager) error {
				args := map[string]interface{}{"issue_key": "PROJ-1"}
				_, err := m.JiraGetIssue(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
			t.Setenv("ATLASSIAN_TOKEN", "test-token")
			t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			m, err := atlassian.NewJiraManager(sm, client)
			require.NoError(t, err)

			err = tt.call(context.Background(), m)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestADOManager_MoreErrorPaths(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		call       func(ctx context.Context, m *ado.AdoManager) error
		wantErrMsg string
	}{
		{
			name: "Unmarshal Failure in adoListPipelineRuns",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"value": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"pipeline_id":  1,
				}
				_, err := m.AdoListPipelineRuns(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in listPipelineLogs",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"value": [{"id": "not-an-int"}]}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"pipeline_id":  1,
					"run_id":       1,
				}
				_, err := m.AdoGetPipelineLogs(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode logs list",
		},
		{
			name: "Unmarshal Failure in adoGetBuildTimeline",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"records": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"build_id":     1,
				}
				_, err := m.AdoGetBuildTimeline(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in adoGetPipelineDefinition",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{invalid}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"pipeline_id":  1,
				}
				_, err := m.AdoGetPipelineDefinition(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in adoUpdateBuildDefinitionVariables (GET)",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{invalid}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization":  "org",
					"project":       "proj",
					"definition_id": 1,
					"variables":     map[string]interface{}{"K": map[string]interface{}{"value": "V"}},
				}
				_, err := m.AdoUpdateBuildDefinitionVariables(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode definition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			m := ado.NewADOManager(sm,
				ado.WithHTTPClient(client),
				ado.WithToken("test-pat"),
			)

			err := tt.call(context.Background(), m)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestADOManager_PolicyErrorPaths(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		call       func(ctx context.Context, m *ado.AdoManager) error
		wantErrMsg string
	}{
		{
			name: "Unmarshal Failure in fetchPrStatuses",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"value": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization":    "org",
					"project":         "proj",
					"repository":      "repo",
					"pull_request_id": 123,
				}
				_, err := m.AdoGetPrStatuses(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in fetchPrProjectID",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"repository": "not-an-object"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization":    "org",
					"project":         "proj",
					"repository":      "repo",
					"pull_request_id": 123,
				}
				_, err := m.AdoGetPrPolicyEvaluations(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode PR metadata",
		},
		{
			name: "Unmarshal Failure in fetchPolicyEvaluations",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Path, "pullrequests/123") {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(`{"repository": {"project": {"id": "proj-id"}}}`)),
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"value": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization":    "org",
					"project":         "proj",
					"repository":      "repo",
					"pull_request_id": 123,
				}
				_, err := m.AdoGetPrPolicyEvaluations(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			m := ado.NewADOManager(sm,
				ado.WithHTTPClient(client),
				ado.WithToken("test-pat"),
			)

			err := tt.call(context.Background(), m)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestADOManager_PrErrorPaths(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	tests := []struct {
		name       string
		setupMock  func(m *mockRoundTripper)
		call       func(ctx context.Context, m *ado.AdoManager) error
		wantErrMsg string
	}{
		{
			name: "Unmarshal Failure in adoListPullRequests",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"value": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization": "org",
					"project":      "proj",
					"repository":   "repo",
				}
				_, err := m.AdoListPullRequests(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
		{
			name: "Unmarshal Failure in adoGetPrThreads",
			setupMock: func(m *mockRoundTripper) {
				m.roundTrip = func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"value": "not-a-slice"}`)),
					}, nil
				}
			},
			call: func(ctx context.Context, m *ado.AdoManager) error {
				args := map[string]interface{}{
					"organization":    "org",
					"project":         "proj",
					"repository":      "repo",
					"pull_request_id": 123,
				}
				_, err := m.AdoGetPrThreads(ctx, args, nil)
				return err
			},
			wantErrMsg: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &mockRoundTripper{}
			if tt.setupMock != nil {
				tt.setupMock(rt)
			}
			client := &http.Client{Transport: rt}

			m := ado.NewADOManager(sm,
				ado.WithHTTPClient(client),
				ado.WithToken("test-pat"),
			)

			err := tt.call(context.Background(), m)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestAtlassianProvider_WaitTime(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	p, err := atlassian.NewAtlassianProvider()
	require.NoError(t, err)
	p.BaseDelay = 0 // Test baseDelay == 0 case

	t.Run("Default baseDelay", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		wait := p.GetWaitTime(resp, 0)
		assert.Equal(t, 1*time.Second, wait)
	})

	t.Run("Invalid Retry-After", func(t *testing.T) {
		p.BaseDelay = 1 * time.Second
		resp := &http.Response{Header: http.Header{"Retry-After": []string{"invalid"}}}
		wait := p.GetWaitTime(resp, 1)
		assert.Equal(t, 2*time.Second, wait) // exponential backoff
	})
}
