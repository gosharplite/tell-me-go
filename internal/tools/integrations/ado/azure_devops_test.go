// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

func TestAdoTools_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	commonArgs := map[string]interface{}{
		"organization": "myorg",
		"project":      "myproj",
		"repository":   "myrepo",
	}

	tests := []struct {
		name           string
		toolFunc       func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
		args           map[string]interface{}
		httpStatus     int
		respBody       string
		doErr          error
		expectedErrMsg string
		setupPAT       string
	}{
		{
			name: "AdoGetPrDiff - Unmarshal Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"pull_request_id": "invalid"}, // should be int
			expectedErrMsg: "json: cannot unmarshal string into Go struct field",
		},
		{
			name: "AdoGetPrDiff - Missing Params",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "AdoGetPrDiff - Request Failure",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			doErr:          fmt.Errorf("network error"),
			expectedErrMsg: "request failed",
		},
		{
			name: "AdoGetPrDiff - 401 Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetPrDiff - 403 Forbidden",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "AdoGetPrDiff - 404 Not Found",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "AdoGetPrDiff - 500 Internal Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusInternalServerError,
			respBody:       "internal error",
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "GetTaskLog - Unmarshal Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getTaskLog(ctx, args, hb)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"build_id": "invalid"},
			expectedErrMsg: "json: cannot unmarshal string into Go struct field",
		},
		{
			name: "GetTaskLog - Missing Params",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getTaskLog(ctx, args, hb)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "GetTaskLog - 404 Not Found",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getTaskLog(ctx, args, hb)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "log_id": 1},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "not found",
		},
		{
			name: "AdoGetBuildTimeline - Missing Params",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.GetBuildTimeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "AdoGetBuildTimeline - 401 Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.GetBuildTimeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetPrPolicyEvaluations - PR Metadata Failure",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrPolicyEvaluations(ctx, args, nil)
			},
			args: map[string]interface{}{
				"organization":    "myorg",
				"project":         "myproj",
				"repository":      "myrepo",
				"pull_request_id": 123,
			},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "AdoListPullRequests - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoListPullRequests(ctx, args, nil)
			},
			args:           commonArgs,
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoListPullRequests - Forbidden",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoListPullRequests(ctx, args, nil)
			},
			args:           commonArgs,
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "AdoListPullRequests - Not Found",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoListPullRequests(ctx, args, nil)
			},
			args:           commonArgs,
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "AdoGetPrThreads - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrThreads(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetPrThreads - Forbidden",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrThreads(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "AdoGetPrThreads - Not Found",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrThreads(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "AdoListRepositoryItems - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoListRepositoryItems(ctx, args, nil)
			},
			args:           commonArgs,
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoListRepositoryItems - Forbidden",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoListRepositoryItems(ctx, args, nil)
			},
			args:           commonArgs,
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "AdoListRepositoryItems - Not Found",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoListRepositoryItems(ctx, args, nil)
			},
			args:           commonArgs,
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "ListPipelineRuns - 500 Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, _, err := m.ListPipelineRuns(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "GetPipelineRun - 500 Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.GetPipelineRun(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "AdoGetBuildChanges - 500 Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getBuildChanges(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "AdoGetBuildChanges - 401 Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getBuildChanges(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetPrStatuses - 401 Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrStatuses(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetPrStatuses - 403 Forbidden",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrStatuses(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "AdoGetPrPolicyEvaluations - 403 Forbidden",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrPolicyEvaluations(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "AdoGetPrPolicyEvaluations - PR Metadata Decode Failure",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.AdoGetPrPolicyEvaluations(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusOK,
			respBody:       `{invalid}`,
			expectedErrMsg: "failed to decode PR metadata",
		},
		{
			name: "AdoGetFileContent - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetFileContent(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetFileContent - Default Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return m.adoGetFileContent(ctx, args, nil)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "ListPipelineLogs - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, _, err := m.ListPipelineLogs(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetBuildTimeline - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.GetBuildTimeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "GetTaskLog - Unauthorized",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getTaskLog(ctx, args, hb)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "log_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "AdoGetBuildChanges - Not Found",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.getBuildChanges(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "CreatePipeline - Missing Params",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.createPipeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "name": "n", "repository_id": "r"}, // Missing yaml_path
			expectedErrMsg: "required",
		},
		{
			name: "CreatePipeline - 500 Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				// Approved by default in setup
				_, err := m.createPipeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args: map[string]interface{}{
				"organization": "o", "project": "p", "name": "n", "repository_id": "r", "yaml_path": "y",
			},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "CreatePipeline - Malformed JSON",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.createPipeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args: map[string]interface{}{
				"organization": "o", "project": "p", "name": "n", "repository_id": "r", "yaml_path": "y",
			},
			httpStatus:     http.StatusOK,
			respBody:       `{bad_json}`,
			expectedErrMsg: "failed to decode response",
		},
		{
			name: "RunPipeline - Missing Params",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.runPipeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p"}, // Missing pipeline_id
			expectedErrMsg: "required",
		},
		{
			name: "RunPipeline - 500 Error",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.runPipeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "RunPipeline - Malformed JSON",
			toolFunc: func(m *AdoManager, ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				_, err := m.runPipeline(ctx, args)
				return tools.ToolResult{}, err
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1},
			httpStatus:     http.StatusOK,
			respBody:       `{bad_json}`,
			expectedErrMsg: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pat := tt.setupPAT
			if pat == "" {
				pat = "test-pat"
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.httpStatus != 0 {
					w.WriteHeader(tt.httpStatus)
				} else {
					w.WriteHeader(http.StatusOK)
				}
				if tt.respBody != "" {
					_, _ = w.Write([]byte(tt.respBody))
				}
			}))
			t.Cleanup(server.Close)

			m := NewADOManager(sm, WithBaseURL(server.URL), WithToken(pat))

			if tt.doErr != nil {
				// To simulate transport error, we can close the server immediately
				// or use a client that always returns error.
				// Since we want to test actual transport, let's close the server.
				server.Close()
			}

			_, err := tt.toolFunc(m, context.Background(), tt.args, nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
		})
	}

	t.Run("AdoGetPrPolicyEvaluations - Policy List Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/pullrequests/123") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"repository": {"project": {"id": "proj-guid"}}}`))
				return
			}
			// Second call fails
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		_, err := m.AdoGetPrPolicyEvaluations(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "azure DevOps API returned status: 500")
	})

	t.Run("AdoListBranchPolicies - Policy Config Fetch Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/_apis/git/repositories/myrepo") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": "repo-guid"}`))
				return
			}
			// Second call fails
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"branch_name":  "main",
		}

		_, err := m.adoListBranchPolicies(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch policy configurations")
	})
}

func TestAdoTools_AuthError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	m := NewADOManager(sm)
	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "o",
		"project":      "p",
		"repository":   "r",
	}

	_, err := m.AdoListPullRequests(ctx, args, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AZURE_PAT_ALL token is required but not provided")
}

func TestAdoTools_MissingParams(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	m := NewADOManager(sm)
	ctx := context.Background()

	t.Run("AdoGetFileContent", func(t *testing.T) {
		_, err := m.adoGetFileContent(ctx, map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("AdoGetBuildChanges", func(t *testing.T) {
		_, err := m.getBuildChanges(ctx, map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("AdoGetPrStatuses", func(t *testing.T) {
		_, err := m.AdoGetPrStatuses(ctx, map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})
}

func TestAdoGetPullRequest_UnmarshalError(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	m := NewADOManager(nil)
	_, err := m.adoGetPullRequest(context.Background(), map[string]interface{}{"pull_request_id": "invalid"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestAzureDevOps_JSONDecodeErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{ invalid json"))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
	ctx := context.Background()

	// Define standard args that satisfy validation for most endpoints
	args := map[string]interface{}{
		"organization":    "myorg",
		"project":         "myproj",
		"repository":      "myrepo",
		"pull_request_id": 123,
		"pipeline_id":     123,
		"run_id":          456,
		"build_id":        789,
		"path":            "/src/main.go",
		"branch_name":     "main",
	}

	tests := []struct {
		name string
		call func() (tools.ToolResult, error)
	}{
		{"AdoListPullRequests", func() (tools.ToolResult, error) { return m.AdoListPullRequests(ctx, args, nil) }},
		{"AdoGetPullRequest", func() (tools.ToolResult, error) { return m.adoGetPullRequest(ctx, args, nil) }},
		{"AdoGetPrDiff", func() (tools.ToolResult, error) { return m.adoGetPrDiff(ctx, args, nil) }},
		{"AdoGetPrThreads", func() (tools.ToolResult, error) { return m.AdoGetPrThreads(ctx, args, nil) }},
		{"AdoListRepositoryItems", func() (tools.ToolResult, error) { return m.AdoListRepositoryItems(ctx, args, nil) }},
		{"ListPipelineRuns", func() (tools.ToolResult, error) {
			_, _, err := m.ListPipelineRuns(ctx, args)
			return tools.ToolResult{}, err
		}},
		{"GetPipelineRun", func() (tools.ToolResult, error) {
			_, err := m.GetPipelineRun(ctx, args)
			return tools.ToolResult{}, err
		}},
		{"ListPipelineLogs", func() (tools.ToolResult, error) {
			_, _, err := m.ListPipelineLogs(ctx, args)
			return tools.ToolResult{}, err
		}},
		{"AdoGetPrStatuses", func() (tools.ToolResult, error) { return m.AdoGetPrStatuses(ctx, args, nil) }},
		{"AdoGetPrPolicyEvaluations", func() (tools.ToolResult, error) { return m.AdoGetPrPolicyEvaluations(ctx, args, nil) }},
		{"AdoListBranchPolicies", func() (tools.ToolResult, error) { return m.adoListBranchPolicies(ctx, args, nil) }},
		{"AdoGetBuildTimeline", func() (tools.ToolResult, error) {
			_, err := m.GetBuildTimeline(ctx, args)
			return tools.ToolResult{}, err
		}},
		{"AdoGetBuildChanges", func() (tools.ToolResult, error) {
			_, err := m.getBuildChanges(ctx, args)
			return tools.ToolResult{}, err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.call()
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), "decode")
			}
		})
	}
}

func TestAzureDevOps_HTTPIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	ctx := context.Background()

	tests := []struct {
		name         string
		handler      func(w http.ResponseWriter, r *http.Request)
		call         func(m *AdoManager) (tools.ToolResult, error)
		expectedText string
		expectedErr  string
	}{
		{
			name: "Success - GetPullRequest",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"title": "PR Title", "status": "active", "createdBy": {"displayName": "User"}, "creationDate": "2023-01-01", "repository": {"name": "repo"}}`))
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				}, nil)
			},
			expectedText: "PR Title",
		},
		{
			name: "Success - ListPipelineRuns",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"value": [{"id": 1, "buildNumber": "run1", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo"}}]}`))
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				_, runs, err := m.ListPipelineRuns(ctx, map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1})
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: fmt.Sprintf("Run ID: %d", runs[0].Id)}, nil
			},
			expectedText: "Run ID: 1",
		},
		{
			name: "Error - 500 Internal Server Error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				}, nil)
			},
			expectedErr: "returned status: 500",
		},
		{
			name: "Error - 503 Service Unavailable",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("service unavailable"))
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				}, nil)
			},
			expectedErr: "returned status: 503",
		},
		{
			name: "Error - 504 Gateway Timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("gateway timeout"))
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				}, nil)
			},
			expectedErr: "returned status: 504",
		},
		{
			name: "Error - Context Cancellation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(200 * time.Millisecond):
				}
				w.WriteHeader(http.StatusOK)
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				childCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
				return m.adoGetPullRequest(childCtx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				}, nil)
			},
			expectedErr: "context deadline exceeded",
		},
		{
			name: "Error - Malformed JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{ malformed`))
			},
			call: func(m *AdoManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				}, nil)
			},
			expectedErr: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			t.Cleanup(server.Close)

			m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

			result, err := tt.call(m)
			if tt.expectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
			} else {
				assert.NoError(t, err)
				assert.Contains(t, result.Text, tt.expectedText)
			}
		})
	}
}
