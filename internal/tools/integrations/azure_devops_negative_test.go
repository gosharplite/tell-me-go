// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdoManager_ExecuteCreatePipeline_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &mockSecurityManager{approved: true}

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

			m := newADOManager(sm,
				WithBaseURL(ts.URL),
				WithHTTPClient(ts.Client()),
				withToken("test-pat"),
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

func TestAdoManager_ExecuteRequest_NetworkError(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &mockSecurityManager{approved: true}

	// We use a client that points to a non-existent port or closed server
	m := newADOManager(sm,
		WithBaseURL("http://127.0.0.1:1"), // Likely to fail
		withToken("test-pat"),
	)

	ctx := context.Background()
	_, err := m.executeRequest(ctx, http.MethodGet, m.baseURL, nil, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestAdoManager_ExecuteRequest_AuthMissing(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	m := newADOManager(sm)

	ctx := context.Background()
	_, err := m.executeRequest(ctx, http.MethodGet, "http://example.com", nil, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AZURE_PAT_ALL token is required but not provided")
}

func TestAdoManager_AdoGetPipelineRun_Errors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &mockSecurityManager{approved: true}

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

			m := newADOManager(sm,
				WithBaseURL(ts.URL),
				WithHTTPClient(ts.Client()),
				withToken("test-pat"),
			)

			args := map[string]interface{}{
				"organization": "org",
				"project":      "proj",
				"pipeline_id":  1,
				"run_id":       101,
			}
			_, err := m.adoGetPipelineRun(context.Background(), args)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAdoManager_ResolvePipelineID_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Fetch Pipelines Failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
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

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		_, err := m.resolvePipelineID(context.Background(), "org", "proj", "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestAdoManager_ExecuteRunPipeline_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Malformed JSON Response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{ "id": "not-an-int" }`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		_, _, err := m.executeRunPipeline(context.Background(), "org", "proj", 1, "ref", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})

	t.Run("HTTP Error Status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusGatewayTimeout)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		_, _, err := m.executeRunPipeline(context.Background(), "org", "proj", 1, "ref", nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 504")
	})
}

func TestAdoManager_AdoListRepositoryItems_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoListRepositoryItems(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.adoListRepositoryItems(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": ["invalid", "schema"]}`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.adoListRepositoryItems(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_AdoListPipelineRuns_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoListPipelineRuns(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, err := m.adoListPipelineRuns(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [{"id": "not-an-int"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, err := m.adoListPipelineRuns(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_AdoGetPipelineLogs_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("List Logs - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		_, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("List Logs - JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [{"id": "not-an-int"}]}`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		_, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode logs list")
	})

	t.Run("Fetch Log Content - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 1}
		_, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoManager_AdoListBranchPolicies_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Fetch Repository ID - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}
		_, err := m.adoListBranchPolicies(context.Background(), args)
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

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}
		_, err := m.adoListBranchPolicies(context.Background(), args)
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

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}
		_, err := m.adoListBranchPolicies(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode policy configurations")
	})
}

func TestAdoManager_AdoListPullRequests_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoListPullRequests(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.adoListPullRequests(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 502")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": "not-a-slice"}`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r"}
		_, err := m.adoListPullRequests(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_AdoGetPrPolicyEvaluations_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoGetPrPolicyEvaluations(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Fetch PR Metadata - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123}
		_, err := m.adoGetPrPolicyEvaluations(context.Background(), args)
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

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123}
		_, err := m.adoGetPrPolicyEvaluations(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

func TestAdoManager_AdoGetPipelineDefinition_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoGetPipelineDefinition(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, err := m.adoGetPipelineDefinition(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{ bad json }`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		_, err := m.adoGetPipelineDefinition(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})
}

func TestAdoManager_AdoUpdateBuildDefinitionVariables_Errors(t *testing.T) {
	sm := &mockSecurityManager{approved: true}

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newADOManager(sm)
		_, err := m.adoUpdateBuildDefinitionVariables(context.Background(), map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("GET Build Definition - HTTP Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.adoUpdateBuildDefinitionVariables(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("GET Build Definition - JSON Decode Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{ invalid json }`))
		}))
		t.Cleanup(ts.Close)

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.adoUpdateBuildDefinitionVariables(context.Background(), args)
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

		m := newADOManager(sm, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		_, err := m.adoUpdateBuildDefinitionVariables(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 400")
	})

	t.Run("Confirmation Denied", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 1, "variables": {}}`))
		}))
		t.Cleanup(ts.Close)

		deniedSM := &mockSecurityManager{approved: false}
		m := newADOManager(deniedSM, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()), withToken("test-pat"))
		args := map[string]interface{}{
			"organization":  "o",
			"project":       "p",
			"definition_id": 1,
			"variables": map[string]interface{}{
				"TEST": map[string]interface{}{"value": "val"},
			},
		}
		res, err := m.adoUpdateBuildDefinitionVariables(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, res.Text, "cancelled by user")
	})
}
