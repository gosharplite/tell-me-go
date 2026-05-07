// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopHeartbeat returns a buffered heartbeat channel that drains in the background.
func noopHeartbeat() chan<- struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		for range ch {
		}
	}()
	return ch
}

// setupADOServer creates an httptest server, registers cleanup, and returns
// an AdoManager pointed at it with an already-approved or rejected security confirmer.
func setupADOServer(t *testing.T, handler http.HandlerFunc, approved bool) *AdoManager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewADOManager(&mockSecurityManager{approved: approved}, WithBaseURL(server.URL), WithToken("test-pat"))
}

// ============================================================================
// Build handlers (build.go)
// ============================================================================

func TestNewGetBuildTimelineHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/build/builds/1/timeline")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"records":[{"id":"1","name":"Task 1"}]}`))
		}, true)

		handler := newGetBuildTimelineHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Task 1")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, true)

		handler := newGetBuildTimelineHandler(m)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1,
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestNewGetTaskLogHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/build/builds/1/logs/5")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("build output\n"))
		}, true)

		handler := newGetTaskLogHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1, "log_id": 5,
		}, noopHeartbeat())

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "build output")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, true)

		handler := newGetTaskLogHandler(m)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1, "log_id": 5,
		}, noopHeartbeat())

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestNewGetBuildChangesHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/build/builds/1/changes")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":"abc","message":"feat: add"}]}`))
		}, true)

		handler := newGetBuildChangesHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "feat: add")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}, true)

		handler := newGetBuildChangesHandler(m)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1,
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

// ============================================================================
// Pipeline handlers (pipeline.go)
// ============================================================================

func TestNewListPipelinesHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/_apis/pipelines")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":1,"name":"my-pipeline"}]}`))
		}, true)

		f := newPipelineFormatter()
		handler := newListPipelinesHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p",
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Found 1 pipelines")
		assert.Contains(t, result.Text, "[1] my-pipeline")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, true)

		f := newPipelineFormatter()
		handler := newListPipelinesHandler(m, f)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p",
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestNewGetPipelineRunHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/runs/101")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":101,"name":"run1","state":"completed","result":"succeeded","createdDate":"d","url":"u"}`))
		}, true)

		f := newPipelineFormatter()
		handler := newGetPipelineRunHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Pipeline Run #101 Details:")
		assert.Contains(t, result.Text, "Name: run1")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}, true)

		f := newPipelineFormatter()
		handler := newGetPipelineRunHandler(m, f)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101,
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestNewGetPipelineDefinitionHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/_apis/pipelines/123")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":123,"name":"test-pipeline"}`))
		}, true)

		handler := newGetPipelineDefinitionHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 123,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "test-pipeline")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}, true)

		handler := newGetPipelineDefinitionHandler(m)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 123,
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 400")
	})
}

func TestNewListPipelineRunsHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/_apis/build/builds")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":101,"buildNumber":"r1","status":"completed","result":"succeeded","queueTime":"t","repository":{"name":"repo"}}]}`))
		}, true)

		f := newPipelineFormatter()
		handler := newListPipelineRunsHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Recent runs for pipeline")
		assert.Contains(t, result.Text, "Run ID: 101")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, true)

		f := newPipelineFormatter()
		handler := newListPipelineRunsHandler(m, f)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1,
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestNewGetPipelineLogsHandler(t *testing.T) {
	t.Run("Log listing (no log_id)", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/logs")
			assert.NotContains(t, r.URL.Path, "/logs/")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":1,"lineCount":10}]}`))
		}, true)

		f := newPipelineFormatter()
		handler := newGetPipelineLogsHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Logs for Pipeline Run")
		assert.Contains(t, result.Text, "Log ID: 1")
		assert.Contains(t, result.Text, "provide a log_id")
	})

	t.Run("Log content (with log_id)", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/logs/5")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("log content here"))
		}, true)

		f := newPipelineFormatter()
		handler := newGetPipelineLogsHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 5,
		}, noopHeartbeat())

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "log content here")
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, true)

		f := newPipelineFormatter()
		handler := newGetPipelineLogsHandler(m, f)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101,
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestNewCreatePipelineHandler(t *testing.T) {
	t.Run("Already existed", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/_apis/pipelines")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":99,"name":"my-pipe"}]}`))
		}, true)

		handler := newCreatePipelineHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "name": "my-pipe", "repository_id": "r", "yaml_path": "y",
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "already exists with ID: 99")
	})

	t.Run("Cancelled", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":1,"name":"other"}]}`))
		}, false)

		handler := newCreatePipelineHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "name": "my-pipe", "repository_id": "r", "yaml_path": "y",
		}, nil)

		assert.NoError(t, err)
		assert.Equal(t, "Pipeline creation cancelled by user.", result.Text)
	})

	t.Run("Created", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"value":[{"id":1,"name":"other"}]}`))
				return
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":200}`))
				return
			}
		}, true)

		handler := newCreatePipelineHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "name": "new-pipe", "repository_id": "r", "yaml_path": "y",
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Successfully created pipeline")
		assert.Contains(t, result.Text, "200")
	})
}

func TestNewRunPipelineHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/_apis/pipelines/1/runs")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":303,"_links":{"web":{"href":"https://dev.azure.com/x"}}}`))
		}, true)

		f := newPipelineFormatter()
		handler := newRunPipelineHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Successfully triggered pipeline run ID: 303")
		assert.Contains(t, result.Text, "https://dev.azure.com/x")
	})

	t.Run("Cancelled", func(t *testing.T) {
		t.Parallel()
		m := NewADOManager(&mockSecurityManager{approved: false}, WithToken("test-pat"))
		f := newPipelineFormatter()
		handler := newRunPipelineHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Equal(t, "Pipeline run cancelled by user.", result.Text)
	})

	t.Run("Branch formatting", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			var payload map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&payload)
			require.NoError(t, err)

			resources := payload["resources"].(map[string]interface{})
			repos := resources["repositories"].(map[string]interface{})
			self := repos["self"].(map[string]interface{})
			assert.Equal(t, "refs/heads/feature", self["refName"])

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":404,"_links":{"web":{"href":"https://dev.azure.com/x"}}}`))
		}, true)

		f := newPipelineFormatter()
		handler := newRunPipelineHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "branch": "feature",
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Successfully triggered pipeline run ID: 404")
	})
}

// ============================================================================
// Policy handlers (policy.go)
// ============================================================================

func TestNewUpdateBuildDefinitionVariablesHandler(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		var getCalled, putCalled bool
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/_apis/build/definitions/123")
			if r.Method == http.MethodGet {
				getCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":123,"variables":{}}`))
				return
			}
			if r.Method == http.MethodPut {
				putCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
				return
			}
		}, true)

		handler := newUpdateBuildDefinitionVariablesHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "definition_id": 123,
			"variables": map[string]interface{}{
				"new-var": map[string]interface{}{
					"value": "val", "isSecret": false, "allowOverride": false,
				},
			},
		}, nil)

		assert.NoError(t, err)
		assert.True(t, getCalled)
		assert.True(t, putCalled)
		assert.Contains(t, result.Text, "Successfully updated variables for build definition 123")
	})

	t.Run("Cancelled", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":123,"variables":{}}`))
		}, false)

		handler := newUpdateBuildDefinitionVariablesHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "definition_id": 123,
			"variables": map[string]interface{}{
				"new-var": map[string]interface{}{
					"value": "val", "isSecret": false,
				},
			},
		}, nil)

		assert.NoError(t, err)
		assert.Equal(t, "Update cancelled by user.", result.Text)
	})

	t.Run("Error propagates", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, true)

		handler := newUpdateBuildDefinitionVariablesHandler(m)
		_, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "definition_id": 123,
			"variables": map[string]interface{}{
				"new-var": map[string]interface{}{
					"value": "val", "isSecret": false,
				},
			},
		}, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

// ============================================================================
// Edge cases: nil heartbeat channel (handlers should tolerate it)
// ============================================================================

func TestHandlerNilHeartbeat(t *testing.T) {
	t.Run("getTaskLog with nil hb", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("output"))
		}, true)

		handler := newGetTaskLogHandler(m)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "build_id": 1, "log_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Equal(t, "output", strings.TrimSpace(result.Text))
	})

	t.Run("getPipelineLogs content with nil hb", func(t *testing.T) {
		t.Parallel()
		m := setupADOServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("log data"))
		}, true)

		f := newPipelineFormatter()
		handler := newGetPipelineLogsHandler(m, f)
		result, err := handler(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1, "log_id": 1,
		}, nil)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "log data")
	})
}
