// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"

	"github.com/stretchr/testify/assert"
)

func TestAdoManager_ExecuteCreatePipeline_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name           string
		mockStatusCode int
		mockResponse   string
		expectedErr    string
	}{
		{
			name:           "HTTP 500 Internal Server Error",
			mockStatusCode: http.StatusInternalServerError,
			mockResponse:   `{"message": "Something went wrong"}`,
			expectedErr:    "returned status: 500",
		},
		{
			name:           "Malformed JSON Response",
			mockStatusCode: http.StatusOK,
			mockResponse:   `{ bad json: [ }`,
			expectedErr:    "failed to decode response",
		},
		{
			name:           "Unauthorized 401",
			mockStatusCode: http.StatusUnauthorized,
			mockResponse:   `{"message": "Access Denied"}`,
			expectedErr:    "unauthorized",
		},
		{
			name:           "Forbidden 403",
			mockStatusCode: http.StatusForbidden,
			mockResponse:   `{"message": "Forbidden"}`,
			expectedErr:    "forbidden",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatusCode)
				_, _ = w.Write([]byte(tt.mockResponse))
			}))
			t.Cleanup(ts.Close)

			m := NewADOManager(sm,
				WithBaseURL(ts.URL),
				WithHTTPClient(ts.Client()),
				WithToken("test-pat"),
			)

			ctx := context.Background()
			_, err := m.executeCreatePipeline(ctx, "org", "proj", "name", "repo", "path", nil, nil)

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("expected error containing %q, got %q", tt.expectedErr, err.Error())
			}
		})
	}
}

func TestAdoManager_ConfirmationErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	t.Run("createPipeline - confirmation error", func(t *testing.T) {
		t.Parallel()
		// Server returns pipelines list (so checkPipelineExists succeeds with no match)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":1,"name":"other"}]}`))
		}))
		t.Cleanup(ts.Close)

		sm := &toolstest.MockSecurityManager{
			ConfirmFunc: func(ctx context.Context, msg string) (bool, error) {
				return false, fmt.Errorf("confirm I/O failure")
			},
		}
		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "o", "project": "p", "name": "n", "repository_id": "r", "yaml_path": "y",
		}
		_, err := m.createPipeline(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "confirmation error")
	})

	t.Run("runPipeline - confirmation error", func(t *testing.T) {
		t.Parallel()
		sm := &toolstest.MockSecurityManager{
			ConfirmFunc: func(ctx context.Context, msg string) (bool, error) {
				return false, fmt.Errorf("confirm I/O failure")
			},
		}
		m := NewADOManager(sm, WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1,
		}
		_, err := m.runPipeline(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "confirmation error")
	})

	t.Run("UpdateBuildDefinitionVariables - confirmation error", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// GET returns valid definition
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"variables":{}}`))
		}))
		t.Cleanup(ts.Close)

		sm := &toolstest.MockSecurityManager{
			ConfirmFunc: func(ctx context.Context, msg string) (bool, error) {
				return false, fmt.Errorf("confirm I/O failure")
			},
		}
		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.UpdateBuildDefinitionVariables(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "confirmation error")
	})
}

func TestAdoManager_ExecuteRequest_NetworkError(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	// We use a client that points to a non-existent port or closed server
	m := NewADOManager(sm,
		WithBaseURL("http://127.0.0.1:1"), // Likely to fail
		WithToken("test-pat"),
	)

	ctx := context.Background()
	_, err := m.ExecuteRequest(ctx, http.MethodGet, m.BaseURL, nil, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestAdoManager_ExecuteRequest_AuthMissing(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	m := NewADOManager(sm)

	ctx := context.Background()
	_, err := m.ExecuteRequest(ctx, http.MethodGet, "http://example.com", nil, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AZURE_PAT_ALL token is required but not provided")
}

func TestAdoManager_GetPipelineRun_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name           string
		mockStatusCode int
		mockResponse   string
		expectedErr    string
	}{
		{
			name:           "HTTP 404 Not Found",
			mockStatusCode: http.StatusNotFound,
			mockResponse:   `{"message": "Not Found"}`,
			expectedErr:    "resource not found",
		},
		{
			name:           "Malformed JSON",
			mockStatusCode: http.StatusOK,
			mockResponse:   `{ "id": "invalid" }`, // id is expected to be int
			expectedErr:    "failed to decode response",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatusCode)
				_, _ = w.Write([]byte(tt.mockResponse))
			}))
			t.Cleanup(ts.Close)

			m := NewADOManager(sm,
				WithBaseURL(ts.URL),
				WithHTTPClient(ts.Client()),
				WithToken("test-pat"),
			)

			args := map[string]interface{}{
				"organization": "org",
				"project":      "proj",
				"pipeline_id":  1,
				"run_id":       101,
			}
			_, err := m.GetPipelineRun(context.Background(), args)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAdoManager_ResolvePipelineID_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Fetch Pipelines Failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		_, err := m.resolvePipelineID(context.Background(), "org", "proj", "name")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch pipelines")
	})

	t.Run("Pipeline Not Found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [{"id": 1, "name": "other"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		_, err := m.resolvePipelineID(context.Background(), "org", "proj", "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Fetch Pipelines - JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		_, err := m.resolvePipelineID(context.Background(), "org", "proj", "name")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_ExecuteRunPipeline_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Malformed JSON Response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{ "id": "not-an-int" }`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		_, _, err := m.executeRunPipeline(context.Background(), "org", "proj", 1, "ref", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})

	t.Run("HTTP Error Status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusGatewayTimeout)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		_, _, err := m.executeRunPipeline(context.Background(), "org", "proj", 1, "ref", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 504")
	})
}

func TestAdoManager_AdoListRepositoryItems_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, err := m.AdoListRepositoryItems(context.Background(), map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.AdoListRepositoryItems(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": ["invalid", "schema"]}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.AdoListRepositoryItems(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_ListPipelineRuns_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, _, err := m.ListPipelineRuns(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, _, err := m.ListPipelineRuns(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [{"id": "not-an-int"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, _, err := m.ListPipelineRuns(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_ListPipelineLogs_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, _, err := m.ListPipelineLogs(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("List Logs - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		_, _, err := m.ListPipelineLogs(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("List Logs - JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [{"id": "not-an-int"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		_, _, err := m.ListPipelineLogs(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode logs list")
	})

	t.Run("Fetch Log Content - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 1}
		_, err := m.getPipelineLogContent(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Fetch Log Content - processLogContent error (scanner.Err)", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return a single line larger than the scanner's max buffer (1MB)
			// to trigger bufio.ErrTooLong wrapped as "log stream interrupted"
			w.WriteHeader(http.StatusOK)
			tooLong := make([]byte, 1*1024*1024+1)
			for i := range tooLong {
				tooLong[i] = 'A'
			}
			_, _ = w.Write(tooLong)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 1}
		_, err := m.getPipelineLogContent(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to process log content")
	})
}

func TestAdoManager_AdoListBranchPolicies_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Fetch Repository ID - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}
		_, err := m.adoListBranchPolicies(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("Fetch Policy Configs - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/_apis/git/repositories") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": "repo-id"}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}
		_, err := m.adoListBranchPolicies(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch policy configurations")
	})

	t.Run("Fetch Policy Configs - JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/_apis/git/repositories") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": "repo-id"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [{"isEnabled": "not-a-bool"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}
		_, err := m.adoListBranchPolicies(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode policy configurations")
	})
}

func TestAdoManager_AdoListPullRequests_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, err := m.AdoListPullRequests(context.Background(), map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.AdoListPullRequests(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 502")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": "not-a-slice"}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.AdoListPullRequests(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_AdoGetPrPolicyEvaluations_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, err := m.AdoGetPrPolicyEvaluations(context.Background(), map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Fetch PR Metadata - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123}
		_, err := m.AdoGetPrPolicyEvaluations(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Fetch Policy Evaluations - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/pullrequests/123") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"repository": {"project": {"id": "proj-id"}}}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123}
		_, err := m.AdoGetPrPolicyEvaluations(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

func TestAdoManager_GetPipelineDefinition_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, err := m.GetPipelineDefinition(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, err := m.GetPipelineDefinition(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{ bad json }`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, err := m.GetPipelineDefinition(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_UpdateBuildDefinitionVariables_Errors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		_, err := m.UpdateBuildDefinitionVariables(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("GET Build Definition - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.UpdateBuildDefinitionVariables(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("GET Build Definition - JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{ invalid json }`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.UpdateBuildDefinitionVariables(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode definition")
	})

	t.Run("PUT Build Definition - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": 1, "variables": {}}`))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.UpdateBuildDefinitionVariables(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 400")
	})

	t.Run("Confirmation Denied", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 1, "variables": {}}`))
		}))
		t.Cleanup(ts.Close)

		deniedSM := &toolstest.MockSecurityManager{ConfirmFunc: func(ctx context.Context, msg string) (bool, error) { return false, nil }}
		m := NewADOManager(deniedSM, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		res, err := m.UpdateBuildDefinitionVariables(context.Background(), args)
		assert.NoError(t, err)
		assert.True(t, res.Cancelled)
		assert.Equal(t, 1, res.DefinitionID)
	})
}

func TestBuildVariablesUpdatePayload_NonMapVariables(t *testing.T) {
	// When existingDef["variables"] exists but is not a map[string]interface{},
	// the function should replace it with an empty map and proceed.
	existingDef := map[string]interface{}{
		"variables": "not-a-map", // wrong type
	}
	inputVars := map[string]adoVariable{
		"MY_VAR": {Value: "x", IsSecret: false},
	}

	body, err := buildVariablesUpdatePayload(existingDef, inputVars)
	assert.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	assert.NoError(t, err)

	vars, ok := result["variables"].(map[string]interface{})
	assert.True(t, ok, "variables should be a map after buildVariablesUpdatePayload")
	assert.Contains(t, vars, "MY_VAR")
}

func TestAdoManager_UnmarshalArgsErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	t.Run("ListPipelineRuns - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, _, err := m.ListPipelineRuns(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": "not-an-int",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing list pipeline runs args")
	})

	t.Run("GetPipelineRun - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, err := m.GetPipelineRun(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": "not-an-int", "run_id": 1,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing get pipeline run args")
	})

	t.Run("getPipelineLogContent - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, err := m.getPipelineLogContent(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": "not-an-int", "run_id": 1, "log_id": 1,
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing get pipeline log content args")
	})
}

func TestAdoManager_RepositoryTools_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("adoGetFileContent - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(sm, WithToken("test-pat"))
		_, err := m.adoGetFileContent(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": "r", "path": 123, // should be string
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing get file content args")
	})

	t.Run("adoGetFileContent - build URL error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(sm, WithBaseURL("http://x\ny"), WithToken("test-pat"))
		_, err := m.adoGetFileContent(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": "r", "path": "/f",
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse base URL")
	})

	t.Run("AdoListRepositoryItems - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(sm, WithToken("test-pat"))
		_, err := m.AdoListRepositoryItems(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": 456, // should be string
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing list repository items args")
	})

	t.Run("AdoListRepositoryItems - build URL error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(sm, WithBaseURL("http://x\ny"), WithToken("test-pat"))
		_, err := m.AdoListRepositoryItems(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": "r",
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "building list repository items URL")
	})
}

func TestAdoManager_PipelineInfra_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	// Gap 1: GetPipelineDefinition — tools.UnmarshalArgs fails when pipeline_id is not an int.
	// No HTTP server required; the error surfaces before any network call.
	t.Run("GetPipelineDefinition - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, err := m.GetPipelineDefinition(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": "not-an-int",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing get pipeline definition args")
	})

	// Gap 2: getBuildChanges — buildGetBuildChangesURL fails when url.Parse rejects a
	// BaseURL containing a newline control character. No HTTP server required.
	t.Run("getBuildChanges - build URL error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithBaseURL("http://x\ny"), WithToken("test-pat"))
		_, err := m.getBuildChanges(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse base URL")
	})

	// Gap 3: ListPipelineRuns — when pipeline_name is provided but no pipeline matches,
	// resolvePipelineID returns "pipeline with name 'X' not found".
	t.Run("ListPipelineRuns - resolvePipelineID error", func(t *testing.T) {
		t.Parallel()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":1,"name":"other-pipeline"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), WithToken("test-pat"))
		_, _, err := m.ListPipelineRuns(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_name": "nonexistent",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resolving pipeline ID")
	})

	// Gap 4: GetBuildTimeline — tools.UnmarshalArgs fails when build_id is not an int.
	// No HTTP server required.
	t.Run("GetBuildTimeline - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, err := m.GetBuildTimeline(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": "not-an-int",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing get build timeline args")
	})
}

func TestAdoManager_FinalErrorPaths(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	// Gap 1: GetPipelineRun — validation error when run_id is missing/zero.
	// No HTTP server needed; fails before any network call.
	t.Run("GetPipelineRun - missing run_id", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, err := m.GetPipelineRun(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1,
			// run_id intentionally omitted — defaults to 0
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "run_id are required")
	})

	// Gap 2: ListPipelineLogs — tools.UnmarshalArgs fails on type mismatch.
	// No HTTP server needed; fails before any network call.
	t.Run("ListPipelineLogs - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, _, err := m.ListPipelineLogs(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": "not-an-int", "run_id": 1,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing list pipeline logs args")
	})

	// Gap 3: AdoGetPrStatuses — tools.UnmarshalArgs fails on type mismatch.
	// No HTTP server needed; fails before any network call.
	t.Run("AdoGetPrStatuses - unmarshal error", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithToken("test-pat"))
		_, err := m.AdoGetPrStatuses(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": "r", "pull_request_id": "not-an-int",
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing get pr statuses args")
	})
}
