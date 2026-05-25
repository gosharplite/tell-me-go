// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdoGetPullRequest(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"title": "Fix bug",
			"status": "active",
			"createdBy": {"displayName": "John Doe"},
			"creationDate": "2023-10-01T12:00:00Z",
			"sourceRefName": "refs/heads/feature",
			"targetRefName": "refs/heads/main",
			"mergeStatus": "succeeded",
			"repository": {"id": "repo-id", "name": "repo-name"}
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(":test-pat"))
			expectedPath := "/myorg/myproj/_apis/git/repositories/myrepo/pullrequests/123"
			assert.Equal(t, expectedPath, r.URL.Path)
			assert.Equal(t, "7.1", r.URL.Query().Get("api-version"))
			assert.Equal(t, expectedAuth, r.Header.Get("Authorization"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		ctx := context.Background()
		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.adoGetPullRequest(ctx, args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Fix bug")
		assert.Contains(t, result.Text, "active")
		assert.Contains(t, result.Text, "John Doe")
		assert.Contains(t, result.Text, "Repository: repo-name (repo-id)")
	})

	t.Run("Unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		_, err := m.adoGetPullRequest(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		_, err := m.adoGetPullRequest(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		args := map[string]interface{}{
			"organization": "myorg",
		}

		_, err := m.adoGetPullRequest(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Missing PAT", func(t *testing.T) {
		m := NewADOManager(sm)
		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		_, err := m.adoGetPullRequest(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AZURE_PAT_ALL token is required but not provided")
	})
}

func TestAdoListPullRequests(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"value": [
				{
					"pullRequestId": 123,
					"title": "Fix bug",
					"createdBy": {"displayName": "John Doe"},
					"creationDate": "2023-10-01T12:00:00Z"
				},
				{
					"pullRequestId": 124,
					"title": "Add feature",
					"createdBy": {"displayName": "Jane Doe"},
					"creationDate": "2023-10-02T12:00:00Z"
				}
			],
			"count": 2
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			assert.Contains(t, r.URL.Path, "/pullrequests")
			assert.Equal(t, "active", q.Get("searchCriteria.status"))
			assert.Equal(t, "50", q.Get("$top"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		ctx := context.Background()
		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
		}

		result, err := m.AdoListPullRequests(ctx, args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Found 2 pull requests")
		assert.Contains(t, result.Text, "[#123] Fix bug")
		assert.Contains(t, result.Text, "[#124] Add feature")
	})

	t.Run("Empty Results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": [], "count": 0}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
		}

		result, err := m.AdoListPullRequests(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, "No pull requests found.", result.Text)
	})

	t.Run("Filters and Top", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			assert.Equal(t, "completed", q.Get("searchCriteria.status"))
			assert.Equal(t, "10", q.Get("$top"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": []}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"status":       "completed",
			"top":          10,
		}

		_, err := m.AdoListPullRequests(context.Background(), args, nil)
		assert.NoError(t, err)
	})
}

func TestAdoGetPrDiff(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"changeEntries": [
				{
					"item": {"path": "/src/main.go"},
					"changeType": "edit"
				},
				{
					"item": {"path": "/src/utils.go"},
					"changeType": "add"
				}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/pullrequests/123/iterations/1/changes")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		ctx := context.Background()
		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.adoGetPrDiff(ctx, args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Total files changed: 2")
		assert.Contains(t, result.Text, "[Edit] /src/main.go")
		assert.Contains(t, result.Text, "[Add] /src/utils.go")
	})

	t.Run("No Changes", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"changeEntries": []}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.adoGetPrDiff(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, "No changes found in this pull request.", result.Text)
	})
}

func TestAdoGetPrThreads(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"value": [
				{
					"isDeleted": false,
					"comments": [
						{
							"author": {"displayName": "John Doe"},
							"content": "Please check this logic.",
							"publishedDate": "2023-10-01T12:00:00Z",
							"commentType": "unknown"
						},
						{
							"author": {"displayName": "Jane Doe"},
							"content": "Looks good to me.",
							"publishedDate": "2023-10-01T12:05:00Z",
							"commentType": "unknown"
						}
					]
				},
				{
					"isDeleted": false,
					"comments": [
						{
							"author": {"displayName": "System"},
							"content": "Build succeeded.",
							"publishedDate": "2023-10-01T12:10:00Z",
							"commentType": "system"
						}
					]
				}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/pullrequests/123/threads")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		ctx := context.Background()
		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.AdoGetPrThreads(ctx, args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Thread 1")
		assert.Contains(t, result.Text, "Please check this logic.")
		assert.Contains(t, result.Text, "Jane Doe: Looks good to me.")
		assert.NotContains(t, result.Text, "Build succeeded.") // System thread filtered
	})

	t.Run("No Threads", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": []}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.AdoGetPrThreads(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, "No discussion threads found in this pull request.", result.Text)
	})
}

func TestAdoGetFileContent(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		fileContent := "package main\n\nfunc main() {}"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			assert.Contains(t, r.URL.Path, "/items")
			assert.Equal(t, "/src/main.go", q.Get("path"))
			assert.Equal(t, "develop", q.Get("versionDescriptor.version"))
			assert.Equal(t, "text", q.Get("$format"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fileContent))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		ctx := context.Background()
		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"path":         "/src/main.go",
			"version":      "develop",
		}

		result, err := m.adoGetFileContent(ctx, args, nil)
		assert.NoError(t, err)
		assert.Equal(t, fileContent, result.Text)
	})

	t.Run("Default Version", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "main", r.URL.Query().Get("versionDescriptor.version"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("content"))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"path":         "/src/main.go",
		}

		_, err := m.adoGetFileContent(context.Background(), args, nil)
		assert.NoError(t, err)
	})

	t.Run("File Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"path":         "/missing.go",
		}

		_, err := m.adoGetFileContent(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoListRepositoryItems(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"value": [
				{"path": "/src", "isFolder": true},
				{"path": "/src/main.go", "isFolder": false},
				{"path": "/README.md", "isFolder": false}
			],
			"count": 3
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			assert.Contains(t, r.URL.Path, "/items")
			assert.Equal(t, "/", q.Get("scopePath"))
			assert.Equal(t, "oneLevel", q.Get("recursionLevel"))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		ctx := context.Background()
		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"recursion_level": "oneLevel",
		}

		result, err := m.AdoListRepositoryItems(ctx, args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "[DIR]  /src")
		assert.Contains(t, result.Text, "[FILE] /src/main.go")
		assert.Contains(t, result.Text, "[FILE] /README.md")
	})

	t.Run("Empty Results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": []}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
		}

		result, err := m.AdoListRepositoryItems(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, "No items found.", result.Text)
	})
}

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

func TestAdoGetPrStatuses(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{
			"value": [
				{
					"state": "succeeded",
					"description": "Build passed",
					"context": {"name": "CI", "genre": "Build"},
					"targetUrl": "http://ci/build/1",
					"creationDate": "2023-10-01T12:00:00Z"
				},
				{
					"state": "failed",
					"description": "Linter failed",
					"context": {"name": "Linter", "genre": "Style"},
					"targetUrl": "http://ci/linter/1",
					"creationDate": "2023-10-01T12:05:00Z"
				}
			]
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/pullrequests/123/statuses")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(jsonResponse))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.AdoGetPrStatuses(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Pull Request #123 Statuses")
		assert.Contains(t, result.Text, "✅ **Build/CI**: succeeded")
		assert.Contains(t, result.Text, "❌ **Style/Linter**: failed")
		assert.Contains(t, result.Text, "Details: http://ci/linter/1")
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 999,
		}

		_, err := m.AdoGetPrStatuses(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetPrPolicyEvaluations(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	jsonResponse := `{
		"value": [
			{
				"status": "broken",
				"configuration": {
					"isEnabled": true,
					"isBlocking": true,
					"type": {"displayName": "Build Validation", "id": "1"},
					"settings": {}
				}
			},
			{
				"status": "approved",
				"configuration": {
					"isEnabled": true,
					"isBlocking": false,
					"type": {"displayName": "Minimum number of reviewers", "id": "2"},
					"settings": {}
				}
			}
		]
	}`

	t.Run("Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/pullrequests/123") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"repository": {"project": {"id": "proj-guid"}}}`))
				return
			}
			if strings.Contains(r.URL.Path, "/_apis/policy/evaluations") {
				assert.Equal(t, "vstfs:///CodeReview/CodeReviewId/proj-guid/123", r.URL.Query().Get("artifactId"))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(jsonResponse))
				return
			}
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.AdoGetPrPolicyEvaluations(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Pull Request #123 Policy Evaluations")
		assert.Contains(t, result.Text, "❌ **Build Validation** [REQUIRED]: broken")
	})

	t.Run("Empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/pullrequests/123") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"repository": {"project": {"id": "proj-guid"}}}`))
				return
			}
			if strings.Contains(r.URL.Path, "/_apis/policy/evaluations") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"value": []}`))
				return
			}
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		result, err := m.AdoGetPrPolicyEvaluations(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Equal(t, "No active policies found for this pull request.", result.Text)
	})

	t.Run("Error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/pullrequests/123") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"repository": {"project": {"id": "proj-guid"}}}`))
				return
			}
			if strings.Contains(r.URL.Path, "/_apis/policy/evaluations") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
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
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

func TestAdoListBranchPolicies(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/_apis/git/repositories/myrepo") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": "repo-guid"}`))
				return
			}
			if strings.Contains(r.URL.Path, "/_apis/policy/configurations") {
				policyResponse := `{
					"value": [
						{
							"isEnabled": true,
							"isBlocking": true,
							"type": {"displayName": "Build"},
							"settings": {
								"scope": [
									{"repositoryId": "repo-guid", "refName": "refs/heads/main"}
								],
								"buildDefinitionId": 19,
								"queueOnSourceUpdateOnly": true
							}
						}
					]
				}`
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(policyResponse))
				return
			}
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"branch_name":  "main",
		}

		result, err := m.adoListBranchPolicies(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Branch Policies for main in myrepo")
		assert.Contains(t, result.Text, "- Type: Build [REQUIRED]")
		assert.Contains(t, result.Text, "Build Definition ID: 19")
		assert.Contains(t, result.Text, "Queue On Source Update Only: true")
	})

	t.Run("No Policies", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/_apis/git/repositories/myrepo") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id": "repo-guid"}`))
				return
			}
			if strings.Contains(r.URL.Path, "/_apis/policy/configurations") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"value": []}`))
				return
			}
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"branch_name":  "main",
		}

		result, err := m.adoListBranchPolicies(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "No active policies found")
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

func TestGetStatusEmoji(t *testing.T) {
	tests := []struct {
		state    string
		expected string
	}{
		{"succeeded", "✅"},
		{"failed", "❌"},
		{"error", "❌"},
		{"pending", "⏳"},
		{"unknown", "⚪"},
		{"", "⚪"},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			assert.Equal(t, tt.expected, getStatusEmoji(tt.state))
		})
	}
}

func TestPolicyMatchesBranch_MissingScope(t *testing.T) {
	t.Run("no scope key", func(t *testing.T) {
		m := &AdoManager{}
		config := adoPolicyConfig{
			Settings: map[string]interface{}{},
		}
		assert.False(t, m.policyMatchesBranch(config, "repo", "ref"))
	})

	t.Run("scope is not a slice", func(t *testing.T) {
		m := &AdoManager{}
		config := adoPolicyConfig{
			Settings: map[string]interface{}{
				"scope": "not a slice",
			},
		}
		assert.False(t, m.policyMatchesBranch(config, "repo", "ref"))
	})

	t.Run("scope contains non-map entry", func(t *testing.T) {
		m := &AdoManager{}
		config := adoPolicyConfig{
			Settings: map[string]interface{}{
				"scope": []interface{}{
					"this is a string, not a map",
					map[string]interface{}{
						"repositoryId": "repo-guid",
						"refName":      "refs/heads/main",
					},
				},
			},
		}
		// The first entry fails the type assertion and is skipped (continue),
		// the second entry matches.
		assert.True(t, m.policyMatchesBranch(config, "repo-guid", "refs/heads/main"))
	})

	t.Run("scope only contains non-map entries", func(t *testing.T) {
		m := &AdoManager{}
		config := adoPolicyConfig{
			Settings: map[string]interface{}{
				"scope": []interface{}{
					"not-a-map",
					12345,
				},
			},
		}
		assert.False(t, m.policyMatchesBranch(config, "repo-guid", "refs/heads/main"))
	})
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

func TestAdoGetPrStatuses_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("fetchPrStatuses - 404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.AdoGetPrStatuses(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("fetchPrStatuses - 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.AdoGetPrStatuses(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestAdoListBranchPolicies_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("fetchRepositoryId - 404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("fetchRepositoryId - Decode Error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid}`))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode repository metadata")
	})
}

func TestPerformPolicyEvaluationRequest_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	t.Run("401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.performPolicyEvaluationRequest(context.Background(), server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.performPolicyEvaluationRequest(context.Background(), server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetFileContent_DefaultStatus(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("teapot"))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(&toolstest.MockSecurityManager{AllowAll: true}, WithBaseURL(server.URL), WithToken("test-pat"))

	_, err := m.adoGetFileContent(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned status: 418")
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

func TestFormatKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simpleKey", "Simple Key"},
		{"URLPath", "URL Path"},
		{"ProjectID", "Project ID"},
		{"MyAPIKey", "My API Key"},
		{"API", "API"},
		{"some_key", "Some_key"}, // underscore not handled by camelCase logic, but first letter capitalized
		{"", ""},
		{"ProjectId", "Project ID"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatKey(tt.input))
		})
	}
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
}
