// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

func TestFormatBranchPolicies(t *testing.T) {
	m := &AdoManager{}

	t.Run("empty configs", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{}, "repo-guid")
		assert.Contains(t, result, "No active policies found")
	})

	t.Run("disabled config", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  false,
				IsBlocking: true,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
		}, "repo-guid")
		assert.Contains(t, result, "No active policies found")
	})

	t.Run("non-matching branch", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/develop"),
			},
		}, "repo-guid")
		assert.Contains(t, result, "No active policies found")
	})

	t.Run("single enabled matching", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
		}, "repo-guid")
		assert.Contains(t, result, "- Type: Build")
		assert.Contains(t, result, "Status: Enabled")
		assert.Contains(t, result, "Branch Policies for main in myrepo")
	})

	t.Run("blocking policy", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: true,
				Type:       adoPolicyType{DisplayName: "RequiredReviewer"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
		}, "repo-guid")
		assert.Contains(t, result, " [REQUIRED]")
	})

	t.Run("non-blocking policy", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
		}, "repo-guid")
		assert.NotContains(t, result, "[REQUIRED]")
	})

	t.Run("branch name normalization", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
		}, "repo-guid")
		assert.Contains(t, result, "- Type: Build")
	})

	t.Run("branch already has refs/heads prefix", func(t *testing.T) {
		result := m.formatBranchPolicies("refs/heads/main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
		}, "repo-guid")
		assert.Contains(t, result, "- Type: Build")
	})

	t.Run("settings with scope key skipped", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings: map[string]interface{}{
					"scope": []interface{}{
						map[string]interface{}{"repositoryId": "repo-guid", "refName": "refs/heads/main"},
					},
					"minimumApproverCount": float64(2),
					"buildDefinitionId":    float64(42),
				},
			},
		}, "repo-guid")
		assert.NotContains(t, result, "scope:")
		assert.NotContains(t, result, "Scope:")
		assert.Contains(t, result, "Minimum Approver Count: 2")
		assert.Contains(t, result, "Build Definition ID: 42")
	})

	t.Run("multiple policies mixed", func(t *testing.T) {
		result := m.formatBranchPolicies("main", "myrepo", []adoPolicyConfig{
			{
				IsEnabled:  false,
				IsBlocking: true,
				Type:       adoPolicyType{DisplayName: "DisabledPolicy"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
			{
				IsEnabled:  true,
				IsBlocking: true,
				Type:       adoPolicyType{DisplayName: "RequiredReviewer"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "Build"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/main"),
			},
			{
				IsEnabled:  true,
				IsBlocking: false,
				Type:       adoPolicyType{DisplayName: "WrongBranch"},
				Settings:   newMatchingScope("repo-guid", "refs/heads/develop"),
			},
		}, "repo-guid")
		assert.NotContains(t, result, "DisabledPolicy")
		assert.NotContains(t, result, "WrongBranch")
		assert.Contains(t, result, "RequiredReviewer")
		assert.Contains(t, result, "Build")
		assert.Equal(t, 2, strings.Count(result, "- Type:"))
	})
}

// newMatchingScope creates a Settings map with a scope entry matching the given repoId and refName.
func newMatchingScope(repoId, refName string) map[string]interface{} {
	return map[string]interface{}{
		"scope": []interface{}{
			map[string]interface{}{
				"repositoryId": repoId,
				"refName":      refName,
			},
		},
	}
}

func TestAdoGetPrStatuses_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name        string
		args        map[string]interface{}
		baseURL     string
		setupServer func() *httptest.Server
		expectedErr string
	}{
		{
			name:        "param validation error",
			args:        map[string]interface{}{},
			expectedErr: "organization, project, repository, and pull_request_id are required",
		},
		{
			name:        "url.Parse error in fetchPrStatuses",
			args:        map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			baseURL:     "http://x\ny",
			expectedErr: "failed to parse statuses base URL",
		},
		{
			name: "ExecuteRequest error in fetchPrStatuses",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			expectedErr: "returned status: 500",
		},
		{
			name: "json.Decode error in fetchPrStatuses",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{invalid`))
				}))
			},
			expectedErr: "failed to decode response",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var m *AdoManager
			if tt.setupServer != nil {
				ts := tt.setupServer()
				t.Cleanup(ts.Close)
				m = NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
			} else if tt.baseURL != "" {
				m = NewADOManager(sm, WithBaseURL(tt.baseURL), WithToken("test-pat"))
			} else {
				m = NewADOManager(sm)
			}

			_, err := m.AdoGetPrStatuses(context.Background(), tt.args, nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAdoGetPrPolicyEvaluations_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name        string
		args        map[string]interface{}
		baseURL     string
		setupServer func() *httptest.Server
		expectedErr string
	}{
		{
			name:        "UnmarshalArgs error",
			args:        map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": "not-an-int"},
			expectedErr: "parsing get pr policy evaluations args",
		},
		{
			name:        "param validation error",
			args:        map[string]interface{}{},
			expectedErr: "organization, project, repository, and pull_request_id are required",
		},
		{
			name: "fetchPrProjectID - HTTP error",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectedErr: "resource not found",
		},
		{
			name: "fetchPrProjectID - json.Decode error",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{invalid`))
				}))
			},
			expectedErr: "failed to decode PR metadata",
		},
		{
			name: "fetchPrProjectID - empty project ID",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"repository":{"project":{"id":""}}}`))
				}))
			},
			expectedErr: "could not find project ID",
		},
		{
			name:        "fetchPolicyEvaluations - url.Parse error",
			args:        map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			baseURL:     "http://x\ny",
			expectedErr: "failed to create request",
		},
		{
			name: "performPolicyEvaluationRequest - HTTP error",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/pullrequests/123") {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"repository":{"project":{"id":"proj-id"}}}`))
						return
					}
					w.WriteHeader(http.StatusUnauthorized)
				}))
			},
			expectedErr: "unauthorized",
		},
		{
			name: "performPolicyEvaluationRequest - json.Decode error",
			args: map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/pullrequests/123") {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"repository":{"project":{"id":"proj-id"}}}`))
						return
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{bad`))
				}))
			},
			expectedErr: "failed to decode response",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var m *AdoManager
			if tt.setupServer != nil {
				ts := tt.setupServer()
				t.Cleanup(ts.Close)
				m = NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
			} else if tt.baseURL != "" {
				m = NewADOManager(sm, WithBaseURL(tt.baseURL), WithToken("test-pat"))
			} else {
				m = NewADOManager(sm)
			}

			_, err := m.AdoGetPrPolicyEvaluations(context.Background(), tt.args, nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAdoListBranchPolicies_UnmarshalError(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))

	args := map[string]interface{}{"organization": 123, "project": "p", "repository": "r", "branch_name": "b"}
	_, err := m.adoListBranchPolicies(context.Background(), args, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing list branch policies args")
}

func TestFormatPrStatuses(t *testing.T) {
	m := &AdoManager{}

	tests := []struct {
		name       string
		prID       int
		statusData adoStatusResponse
		assertFn   func(t *testing.T, result string)
	}{
		{
			name: "empty slice",
			prID: 1,
			statusData: adoStatusResponse{
				Value: []adoStatusItem{},
			},
			assertFn: func(t *testing.T, result string) {
				assert.Equal(t, "No statuses found for this pull request.", result)
			},
		},
		{
			name: "item with genre",
			prID: 10,
			statusData: adoStatusResponse{
				Value: []adoStatusItem{
					{
						State:   "succeeded",
						Context: adoContext{Name: "build", Genre: "ci"},
					},
				},
			},
			assertFn: func(t *testing.T, result string) {
				assert.Contains(t, result, "ci/build")
			},
		},
		{
			name: "item with description",
			prID: 20,
			statusData: adoStatusResponse{
				Value: []adoStatusItem{
					{
						State:       "succeeded",
						Context:     adoContext{Name: "build"},
						Description: "Build completed successfully",
					},
				},
			},
			assertFn: func(t *testing.T, result string) {
				assert.Contains(t, result, "Build completed successfully")
			},
		},
		{
			name: "item with TargetUrl",
			prID: 30,
			statusData: adoStatusResponse{
				Value: []adoStatusItem{
					{
						State:     "succeeded",
						Context:   adoContext{Name: "build"},
						TargetUrl: "https://example.com/details",
					},
				},
			},
			assertFn: func(t *testing.T, result string) {
				assert.Contains(t, result, "Details: https://example.com/details")
			},
		},
		{
			name: "item with all fields populated",
			prID: 40,
			statusData: adoStatusResponse{
				Value: []adoStatusItem{
					{
						State:       "failed",
						Context:     adoContext{Name: "tests", Genre: "ci"},
						Description: "Unit tests failed",
						TargetUrl:   "https://example.com/logs",
					},
				},
			},
			assertFn: func(t *testing.T, result string) {
				assert.Contains(t, result, "ci/tests")
				assert.Contains(t, result, "Unit tests failed")
				assert.Contains(t, result, "Details: https://example.com/logs")
			},
		},
		{
			name: "multiple items",
			prID: 42,
			statusData: adoStatusResponse{
				Value: []adoStatusItem{
					{
						State:   "succeeded",
						Context: adoContext{Name: "Build", Genre: "ci"},
					},
					{
						State:   "pending",
						Context: adoContext{Name: "CodeReview"},
					},
				},
			},
			assertFn: func(t *testing.T, result string) {
				assert.Contains(t, result, "Pull Request #42 Statuses:")
				assert.Contains(t, result, "ci/Build")
				assert.Contains(t, result, "CodeReview")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.formatPrStatuses(tt.prID, tt.statusData)
			tt.assertFn(t, result)
		})
	}
}

func TestFormatPolicyEvaluations(t *testing.T) {
	m := &AdoManager{}

	tests := []struct {
		name       string
		prID       int
		policyData adoPolicyResponse
		assertFn   func(t *testing.T, result tools.ToolResult, err error)
	}{
		{
			name: "empty Value slice",
			prID: 1,
			policyData: adoPolicyResponse{
				Value: []adoPolicyEvaluation{},
			},
			assertFn: func(t *testing.T, result tools.ToolResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "No active policies found for this pull request.", result.Text)
			},
		},
		{
			name: "disabled config skipped",
			prID: 2,
			policyData: adoPolicyResponse{
				Value: []adoPolicyEvaluation{
					{
						Status: "approved",
						Configuration: adoPolicyConfig{
							IsEnabled:  false,
							IsBlocking: true,
							Type:       adoPolicyType{DisplayName: "Build"},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result tools.ToolResult, err error) {
				assert.NoError(t, err)
				assert.Equal(t, "No active policies found for this pull request.", result.Text)
			},
		},
		{
			name: "queued status",
			prID: 3,
			policyData: adoPolicyResponse{
				Value: []adoPolicyEvaluation{
					{
						Status: "queued",
						Configuration: adoPolicyConfig{
							IsEnabled:  true,
							IsBlocking: false,
							Type:       adoPolicyType{DisplayName: "Build"},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result tools.ToolResult, err error) {
				assert.NoError(t, err)
				assert.Contains(t, result.Text, "⏳")
			},
		},
		{
			name: "running status",
			prID: 4,
			policyData: adoPolicyResponse{
				Value: []adoPolicyEvaluation{
					{
						Status: "running",
						Configuration: adoPolicyConfig{
							IsEnabled:  true,
							IsBlocking: false,
							Type:       adoPolicyType{DisplayName: "Build"},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result tools.ToolResult, err error) {
				assert.NoError(t, err)
				assert.Contains(t, result.Text, "⏳")
			},
		},
		{
			name: "IsBlocking=true shows REQUIRED",
			prID: 5,
			policyData: adoPolicyResponse{
				Value: []adoPolicyEvaluation{
					{
						Status: "approved",
						Configuration: adoPolicyConfig{
							IsEnabled:  true,
							IsBlocking: true,
							Type:       adoPolicyType{DisplayName: "RequiredReviewer"},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result tools.ToolResult, err error) {
				assert.NoError(t, err)
				assert.Contains(t, result.Text, " [REQUIRED]")
			},
		},
		{
			name: "multiple policies mixed",
			prID: 6,
			policyData: adoPolicyResponse{
				Value: []adoPolicyEvaluation{
					{
						Status: "approved",
						Configuration: adoPolicyConfig{
							IsEnabled:  false, // disabled — should be skipped
							IsBlocking: true,
							Type:       adoPolicyType{DisplayName: "DisabledPolicy"},
						},
					},
					{
						Status: "queued",
						Configuration: adoPolicyConfig{
							IsEnabled:  true,
							IsBlocking: true,
							Type:       adoPolicyType{DisplayName: "RequiredReviewer"},
						},
					},
					{
						Status: "approved",
						Configuration: adoPolicyConfig{
							IsEnabled:  true,
							IsBlocking: false,
							Type:       adoPolicyType{DisplayName: "Build"},
						},
					},
				},
			},
			assertFn: func(t *testing.T, result tools.ToolResult, err error) {
				assert.NoError(t, err)
				// Disabled policy should not appear
				assert.NotContains(t, result.Text, "DisabledPolicy")
				// Queued+blocking: ⏳ emoji and [REQUIRED]
				assert.Contains(t, result.Text, "⏳")
				assert.Contains(t, result.Text, "RequiredReviewer")
				assert.Contains(t, result.Text, " [REQUIRED]")
				// Approved non-blocking: ✅ emoji, no [REQUIRED] on that line
				assert.Contains(t, result.Text, "✅")
				assert.Contains(t, result.Text, "Build")
				// Exactly 2 policy entries (count "**" pairs — each policy has two: **Name**)
				assert.Equal(t, 4, strings.Count(result.Text, "**"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.formatPolicyEvaluations(tt.prID, tt.policyData)
			tt.assertFn(t, result, err)
		})
	}
}
