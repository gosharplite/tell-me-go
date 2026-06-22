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

	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListPipelineRuns(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{"value": [{"id": 101, "buildNumber": "run1", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "myrepo"}}]}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/_apis/build/builds")
			assert.Equal(t, "1", r.URL.Query().Get("definitions"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		pipelineID, runs, err := m.ListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, 1, pipelineID)
		assert.Len(t, runs, 1)
		assert.Equal(t, 101, runs[0].Id)
		assert.Equal(t, "myrepo", runs[0].Repository.Name)
	})
}

func TestGetPipelineRun(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{"id": 101, "name": "run1", "state": "completed", "result": "succeeded", "createdDate": "2023-10-01", "url": "http://run"}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/pipelines/1/runs/101")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		run, err := m.GetPipelineRun(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, 101, run.Id)
		assert.Equal(t, "run1", run.Name)
		assert.Equal(t, "completed", run.State)
		assert.Equal(t, "succeeded", run.Result)
	})
}

func TestListPipelineLogs(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{"value": [{"id": 1, "lineCount": 10}]}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/runs/101/logs")
			assert.NotContains(t, r.URL.Path, "/logs/1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		runID, logs, err := m.ListPipelineLogs(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, 101, runID)
		assert.Len(t, logs, 1)
		assert.Equal(t, 1, logs[0].Id)
		assert.Equal(t, 10, logs[0].Line)
	})
}

func TestGetPipelineLogContent(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		logContent := "build output"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/runs/101/logs/1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(logContent))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 1}
		content, err := m.getPipelineLogContent(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, logContent, content.Content)
		assert.False(t, content.Truncated)
	})
}

func TestGetBuildTimeline(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"records": [
				{
					"id": "1",
					"name": "Task 1",
					"state": "completed",
					"result": "succeeded",
					"log": {"id": 10}
				}
			]
		}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/build/builds/123/timeline")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123}
		records, err := m.GetBuildTimeline(context.Background(), args)
		assert.NoError(t, err)
		assert.Len(t, records, 1)
		rec, ok := records[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "Task 1", rec["name"])

		log, ok := rec["log"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(10), log["id"])
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123}
		_, err := m.GetBuildTimeline(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestGetTaskLog(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		logContent := "Successfully completed task"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/build/builds/123/logs/10")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(logContent))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "o",
			"project":      "p",
			"build_id":     123,
			"log_id":       10,
		}
		content, err := m.getTaskLog(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, logContent, content.Content)
		assert.False(t, content.Truncated)
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "o",
			"project":      "p",
			"build_id":     123,
			"log_id":       99,
		}
		_, err := m.getTaskLog(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestGetBuildChanges(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"value": [
				{
					"id": "abc123",
					"message": "feat: add something",
					"author": {"displayName": "Developer"}
				}
			]
		}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/build/builds/123/changes")
			assert.Equal(t, "10", r.URL.Query().Get("$top"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123, "top": 10}
		changes, err := m.getBuildChanges(context.Background(), args)
		assert.NoError(t, err)
		assert.Len(t, changes, 1)
		ch, ok := changes[0].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "abc123", ch["id"])
		assert.Equal(t, "feat: add something", ch["message"])
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 999}
		_, err := m.getBuildChanges(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestCreatePipeline(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	tests := []struct {
		name               string
		approved           bool
		existingPipelines  []adoPipeline
		createResponse     string
		expectPost         bool
		expectConfirm      bool
		wantAlreadyExisted bool
		wantCancelled      bool
		wantPipelineID     int
		wantName           string
	}{
		{
			name:     "Success",
			approved: true,
			existingPipelines: []adoPipeline{
				{Id: 1, Name: "Other"},
			},
			createResponse: `{"id": 123}`,
			expectPost:     true,
			expectConfirm:  true,
			wantPipelineID: 123,
			wantName:       "NewPipeline",
		},
		{
			name:     "Idempotency",
			approved: false,
			existingPipelines: []adoPipeline{
				{Id: 1, Name: "NewPipeline"},
			},
			expectPost:         false,
			expectConfirm:      false,
			wantAlreadyExisted: true,
			wantPipelineID:     1,
			wantName:           "NewPipeline",
		},
		{
			name:     "Cancellation",
			approved: false,
			existingPipelines: []adoPipeline{
				{Id: 1, Name: "Other"},
			},
			expectPost:    false,
			expectConfirm: true,
			wantCancelled: true,
			wantName:      "NewPipeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/_apis/pipelines") {
					w.WriteHeader(http.StatusOK)
					resp := struct {
						Value []adoPipeline `json:"value"`
					}{Value: tt.existingPipelines}
					_ = json.NewEncoder(w).Encode(resp)
					return
				}
				if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/_apis/pipelines") {
					postCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tt.createResponse))
					return
				}
			}))
			t.Cleanup(server.Close)

			sm := &toolstest.MockSecurityManager{AllowAll: tt.approved, ConfirmFunc: func(ctx context.Context, msg string) (bool, error) { return tt.approved, nil }}
			m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

			// Pre-populate cache to test invalidation
			cacheKey := "myorg/myproj"
			m.pipelineCache.Store(cacheKey, tt.existingPipelines)

			args := map[string]interface{}{
				"organization":  "myorg",
				"project":       "myproj",
				"name":          "NewPipeline",
				"repository_id": "repo-id",
				"yaml_path":     "/azure-pipelines.yaml",
			}

			result, err := m.createPipeline(context.Background(), args)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantAlreadyExisted, result.AlreadyExisted)
			assert.Equal(t, tt.wantCancelled, result.Cancelled)
			assert.Equal(t, tt.wantPipelineID, result.PipelineID)
			assert.Equal(t, tt.wantName, result.Name)
			assert.Equal(t, tt.expectPost, postCalled, "POST call mismatch")
			assert.Equal(t, tt.expectConfirm, sm.ConfirmCallCount > 0, "Confirm call mismatch")

			if tt.name == "Success" {
				_, exists := m.pipelineCache.Load(cacheKey)
				assert.False(t, exists, "Cache should be invalidated on success")
			}
		})
	}
}

func TestAdoRunPipeline(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"id": 101,
			"_links": {
				"web": {
					"href": "https://dev.azure.com/myorg/myproj/_build/results?buildId=101"
				}
			}
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/_apis/pipelines/1/runs")
			assert.Equal(t, "7.1-preview.1", r.URL.Query().Get("api-version"))

			var payload map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&payload)
			assert.NoError(t, err)

			resources := payload["resources"].(map[string]interface{})
			repos := resources["repositories"].(map[string]interface{})
			self := repos["self"].(map[string]interface{})
			assert.Equal(t, "refs/heads/feature", self["refName"])

			variables := payload["variables"].(map[string]interface{})
			var1 := variables["var1"].(map[string]interface{})
			assert.Equal(t, "val1", var1["value"])
			assert.Equal(t, false, var1["isSecret"])

			params := payload["templateParameters"].(map[string]interface{})
			assert.Equal(t, "paramVal", params["param1"])

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		sm := &toolstest.MockSecurityManager{AllowAll: true}
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		// Branch is the raw user-facing name; _ref_name is the formatted ADO ref.
		args := map[string]interface{}{
			"organization":        "myorg",
			"project":             "myproj",
			"pipeline_id":         1,
			"branch":              "feature",
			"_ref_name":           "refs/heads/feature",
			"variables":           map[string]string{"var1": "val1"},
			"template_parameters": map[string]string{"param1": "paramVal"},
		}

		result, err := m.runPipeline(context.Background(), args)
		assert.NoError(t, err)
		assert.False(t, result.Cancelled)
		assert.Equal(t, 101, result.RunID)
		assert.Equal(t, "https://dev.azure.com/myorg/myproj/_build/results?buildId=101", result.WebURL)
		// Confirmation prompt should show the raw branch, not the formatted ref.
		assert.Contains(t, sm.LastConfirmText, "branch: feature")
		assert.NotContains(t, sm.LastConfirmText, "branch: refs/heads/feature")
	})

	t.Run("Cancelled", func(t *testing.T) {
		sm := &toolstest.MockSecurityManager{ConfirmFunc: func(ctx context.Context, msg string) (bool, error) { return false, nil }}
		m := NewADOManager(sm)

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"pipeline_id":  1,
		}

		result, err := m.runPipeline(context.Background(), args)
		assert.NoError(t, err)
		assert.True(t, result.Cancelled)
	})

	t.Run("FallbackRefName", func(t *testing.T) {
		jsonResponse := `{
			"id": 202,
			"_links": {
				"web": {
					"href": "https://dev.azure.com/myorg/myproj/_build/results?buildId=202"
				}
			}
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&payload)
			assert.NoError(t, err)

			resources := payload["resources"].(map[string]interface{})
			repos := resources["repositories"].(map[string]interface{})
			self := repos["self"].(map[string]interface{})
			// Fallback: RefName defaults to Branch when _ref_name is absent.
			assert.Equal(t, "develop", self["refName"])

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		sm := &toolstest.MockSecurityManager{AllowAll: true}
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		// No _ref_name: exercises the defensive fallback path.
		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"pipeline_id":  1,
			"branch":       "develop",
		}

		result, err := m.runPipeline(context.Background(), args)
		assert.NoError(t, err)
		assert.False(t, result.Cancelled)
		assert.Equal(t, 202, result.RunID)
	})

	t.Run("ConfirmationPromptShowsRawBranch", func(t *testing.T) {
		// Regression test: the confirmation prompt must show the raw
		// user-facing branch name, not the fully qualified ref.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 303, "_links": {"web": {"href": "https://dev.azure.com/x"}}}`))
		}))
		t.Cleanup(server.Close)

		sm := &toolstest.MockSecurityManager{ConfirmFunc: func(ctx context.Context, msg string) (bool, error) { return false, nil }} // decline so no HTTP body assertion needed
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "o",
			"project":      "p",
			"pipeline_id":  1,
			"branch":       "main",
			"_ref_name":    "refs/heads/main",
		}

		result, err := m.runPipeline(context.Background(), args)
		assert.NoError(t, err)
		assert.True(t, result.Cancelled)

		assert.Contains(t, sm.LastConfirmText, "branch: main",
			"confirmation prompt should display the raw branch name")
		assert.NotContains(t, sm.LastConfirmText, "branch: refs/heads/main",
			"confirmation prompt must not display the fully qualified ref")
	})

	// NOTE: The json.Marshal error branch in executeRunPipeline (pipeline_runs.go:154-156)
	// is unreachable with current types (all fields are JSON-safe). This is defense-in-depth
	// dead code tracked by issue #1057.

	t.Run("MinimalPayload", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))

			// Verify resources block still present
			resources := payload["resources"].(map[string]interface{})
			repos := resources["repositories"].(map[string]interface{})
			self := repos["self"].(map[string]interface{})
			assert.Equal(t, "refs/heads/main", self["refName"])

			// Verify optional fields omitted/empty
			_, hasVars := payload["variables"]
			_, hasParams := payload["templateParameters"]
			// Both should be absent when not provided (omitempty)
			assert.False(t, hasVars || hasParams, "variables and templateParameters should be omitted when empty")

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 404, "_links": {"web": {"href": "https://dev.azure.com/x"}}}`))
		}))
		t.Cleanup(server.Close)

		sm := &toolstest.MockSecurityManager{AllowAll: true}
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "o",
			"project":      "p",
			"pipeline_id":  1,
			"branch":       "main",
			"_ref_name":    "refs/heads/main",
		}
		result, err := m.runPipeline(context.Background(), args)
		require.NoError(t, err)
		assert.Equal(t, 404, result.RunID)
	})
}

func TestListPipelineRuns_Features(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Pipeline Name Resolution", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/_apis/pipelines") {
				_, _ = w.Write([]byte(`{"value": [{"id": 42, "name": "my-cool-pipeline"}]}`))
				return
			}
			if strings.Contains(r.URL.Path, "/_apis/build/builds") {
				assert.Equal(t, "42", r.URL.Query().Get("definitions"))
				_, _ = w.Write([]byte(`{"value": [{"id": 101, "buildNumber": "run1", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "myrepo"}}]}`))
				return
			}
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_name": "cool"}
		pipelineID, runs, err := m.ListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, 42, pipelineID)
		assert.Len(t, runs, 1)
	})

	t.Run("Repository Filtering", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "100", r.URL.Query().Get("$top"))
			_, _ = w.Write([]byte(`{"value": [
				{"id": 101, "buildNumber": "run1", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo-a"}},
				{"id": 102, "buildNumber": "run2", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo-b"}}
			]}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "repository": "repo-b"}
		_, runs, err := m.ListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Len(t, runs, 1)
		assert.Equal(t, 102, runs[0].Id)
	})

	t.Run("Limit Truncation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"value": [
				{"id": 101, "buildNumber": "run1", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo-a"}},
				{"id": 102, "buildNumber": "run2", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo-a"}},
				{"id": 103, "buildNumber": "run3", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo-a"}}
			]}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "top": 2}
		_, runs, err := m.ListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Len(t, runs, 2)
		assert.Equal(t, 101, runs[0].Id)
		assert.Equal(t, 102, runs[1].Id)
	})
}

func TestListPipelineRuns_Empty(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value": []}`))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithBaseURL(server.URL), WithToken("test-pat"))

	_, runs, err := m.ListPipelineRuns(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1})
	assert.NoError(t, err)
	assert.Empty(t, runs)
}

func TestGetBuildTimeline_Detailed(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.GetBuildTimeline(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Default", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.GetBuildTimeline(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestGetBuildChanges_Empty(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value": []}`))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithBaseURL(server.URL), WithToken("test-pat"))

	changes, err := m.getBuildChanges(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
	assert.NoError(t, err)
	assert.Empty(t, changes)
}

func TestListPipelineLogs_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("List Path - Request Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		server.Close() // Force failure

		_, _, err := m.ListPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})

	t.Run("List Path - Non-200 Status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, _, err := m.ListPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("List Path - Empty Logs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": []}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		runID, logs, err := m.ListPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.NoError(t, err)
		assert.Equal(t, 1, runID)
		assert.Empty(t, logs)
	})

	t.Run("Content Path - Request Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		server.Close() // Force failure

		_, err := m.getPipelineLogContent(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1, "log_id": 1}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})
}
