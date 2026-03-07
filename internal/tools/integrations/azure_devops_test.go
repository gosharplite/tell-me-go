// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
)

func TestAdoGetPullRequest(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoGetPullRequest(ctx, args)
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

		_, err := m.adoGetPullRequest(context.Background(), args)
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

		_, err := m.adoGetPullRequest(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewADOManager(sm)
		args := map[string]interface{}{
			"organization": "myorg",
		}

		_, err := m.adoGetPullRequest(context.Background(), args)
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

		_, err := m.adoGetPullRequest(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AZURE_PAT_ALL token is required but not provided")
	})
}

func TestAdoListPullRequests(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoListPullRequests(ctx, args)
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

		result, err := m.adoListPullRequests(context.Background(), args)
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

		_, err := m.adoListPullRequests(context.Background(), args)
		assert.NoError(t, err)
	})
}

func TestAdoGetPrDiff(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoGetPrDiff(ctx, args)
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

		result, err := m.adoGetPrDiff(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, "No changes found in this pull request.", result.Text)
	})
}

func TestAdoGetPrThreads(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoGetPrThreads(ctx, args)
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

		result, err := m.adoGetPrThreads(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, "No discussion threads found in this pull request.", result.Text)
	})
}

func TestAdoGetFileContent(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoGetFileContent(ctx, args)
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

		_, err := m.adoGetFileContent(context.Background(), args)
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

		_, err := m.adoGetFileContent(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoListRepositoryItems(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoListRepositoryItems(ctx, args)
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

		result, err := m.adoListRepositoryItems(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, "No items found.", result.Text)
	})
}

func TestAdoListPipelineRuns(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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
		result, err := m.adoListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Run ID: 101")
		assert.Contains(t, result.Text, "Repo: myrepo")
	})
}

func TestAdoGetPipelineRun(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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
		result, err := m.adoGetPipelineRun(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Pipeline Run #101 Details")
	})
}

func TestAdoGetPipelineLogs(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("List Logs", func(t *testing.T) {
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
		result, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Log ID: 1")
	})

	t.Run("Fetch Log Content", func(t *testing.T) {
		logContent := "build output"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/runs/101/logs/1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(logContent))
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 1}
		result, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, logContent, result.Text)
	})
}

func TestAdoGetPrStatuses(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoGetPrStatuses(context.Background(), args)
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

		_, err := m.adoGetPrStatuses(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetPrPolicyEvaluations(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoGetPrPolicyEvaluations(context.Background(), args)
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

		result, err := m.adoGetPrPolicyEvaluations(context.Background(), args)
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

		_, err := m.adoGetPrPolicyEvaluations(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})
}

func TestAdoListBranchPolicies(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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

		result, err := m.adoListBranchPolicies(context.Background(), args)
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

		result, err := m.adoListBranchPolicies(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "No active policies found")
	})
}

func TestAdoGetBuildTimeline(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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
		result, err := m.adoGetBuildTimeline(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, `"name": "Task 1"`)
		assert.Contains(t, result.Text, `"log": {`)
		assert.Contains(t, result.Text, `"id": 10`)
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123}
		_, err := m.adoGetBuildTimeline(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetTaskLog(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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
		result, err := m.adoGetTaskLog(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, logContent, result.Text)
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
		_, err := m.adoGetTaskLog(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetBuildChanges(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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
		result, err := m.adoGetBuildChanges(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, `"id": "abc123"`)
		assert.Contains(t, result.Text, `"message": "feat: add something"`)
	})

	t.Run("Not Found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 999}
		_, err := m.adoGetBuildChanges(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoTools_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := &mockSecurityManager{approved: true}

	commonArgs := map[string]interface{}{
		"organization": "myorg",
		"project":      "myproj",
		"repository":   "myrepo",
	}

	tests := []struct {
		name           string
		toolFunc       func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
		args           map[string]interface{}
		httpStatus     int
		respBody       string
		doErr          error
		expectedErrMsg string
		setupPAT       string
	}{
		{
			name: "adoGetPrDiff - Unmarshal Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"pull_request_id": "invalid"}, // should be int
			expectedErrMsg: "json: cannot unmarshal string into Go struct field",
		},
		{
			name: "adoGetPrDiff - Missing Params",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "adoGetPrDiff - Request Failure",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			doErr:          fmt.Errorf("network error"),
			expectedErrMsg: "request failed",
		},
		{
			name: "adoGetPrDiff - 401 Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrDiff - 403 Forbidden",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrDiff - 404 Not Found",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoGetPrDiff - 500 Internal Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusInternalServerError,
			respBody:       "internal error",
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "adoGetTaskLog - Unmarshal Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"build_id": "invalid"},
			expectedErrMsg: "json: cannot unmarshal string into Go struct field",
		},
		{
			name: "adoGetTaskLog - Missing Params",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "adoGetTaskLog - 404 Not Found",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "log_id": 1},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "not found",
		},
		{
			name: "adoGetBuildTimeline - Missing Params",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildTimeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "adoGetBuildTimeline - 401 Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildTimeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrPolicyEvaluations - PR Metadata Failure",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrPolicyEvaluations(ctx, args)
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
			name: "adoListPullRequests - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPullRequests(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoListPullRequests - Forbidden",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPullRequests(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoListPullRequests - Not Found",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPullRequests(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoGetPrThreads - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrThreads(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrThreads - Forbidden",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrThreads(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrThreads - Not Found",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrThreads(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoListRepositoryItems - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListRepositoryItems(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoListRepositoryItems - Forbidden",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListRepositoryItems(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoListRepositoryItems - Not Found",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListRepositoryItems(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoListPipelineRuns - 500 Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPipelineRuns(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "adoGetPipelineRun - 500 Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPipelineRun(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "adoGetBuildChanges - 500 Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildChanges(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "adoGetBuildChanges - 401 Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildChanges(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrStatuses - 401 Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrStatuses(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrStatuses - 403 Forbidden",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrStatuses(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrPolicyEvaluations - 403 Forbidden",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrPolicyEvaluations(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrPolicyEvaluations - PR Metadata Decode Failure",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrPolicyEvaluations(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusOK,
			respBody:       `{invalid}`,
			expectedErrMsg: "failed to decode PR metadata",
		},
		{
			name: "adoGetFileContent - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetFileContent(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetFileContent - Default Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetFileContent(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "adoGetPipelineLogs - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPipelineLogs(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetBuildTimeline - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildTimeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetTaskLog - Unauthorized",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "log_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetBuildChanges - Not Found",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildChanges(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoCreatePipeline - Missing Params",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoCreatePipeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "name": "n", "repository_id": "r"}, // Missing yaml_path
			expectedErrMsg: "required",
		},
		{
			name: "adoCreatePipeline - 500 Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				// Approved by default in setup
				return m.adoCreatePipeline(ctx, args)
			},
			args: map[string]interface{}{
				"organization": "o", "project": "p", "name": "n", "repository_id": "r", "yaml_path": "y",
			},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "adoCreatePipeline - Malformed JSON",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoCreatePipeline(ctx, args)
			},
			args: map[string]interface{}{
				"organization": "o", "project": "p", "name": "n", "repository_id": "r", "yaml_path": "y",
			},
			httpStatus:     http.StatusOK,
			respBody:       `{bad_json}`,
			expectedErrMsg: "failed to decode response",
		},
		{
			name: "adoRunPipeline - Missing Params",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoRunPipeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p"}, // Missing pipeline_id
			expectedErrMsg: "required",
		},
		{
			name: "adoRunPipeline - 500 Error",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoRunPipeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "adoRunPipeline - Malformed JSON",
			toolFunc: func(m *ADOManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoRunPipeline(ctx, args)
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

			_, err := tt.toolFunc(m, context.Background(), tt.args)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
		})
	}

	t.Run("adoGetPrPolicyEvaluations - Policy List Failure", func(t *testing.T) {
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

		_, err := m.adoGetPrPolicyEvaluations(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "azure DevOps API returned status: 500")
	})

	t.Run("adoListBranchPolicies - Policy Config Fetch Failure", func(t *testing.T) {
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

		_, err := m.adoListBranchPolicies(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch policy configurations")
	})
}

func TestAdoTools_AuthError(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := NewADOManager(sm)
	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "o",
		"project":      "p",
		"repository":   "r",
	}

	_, err := m.adoListPullRequests(ctx, args)
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
	m := &ADOManager{}
	config := adoPolicyConfig{
		Settings: map[string]interface{}{},
	}
	assert.False(t, m.policyMatchesBranch(config, "repo", "ref"))

	config.Settings["scope"] = "not a slice"
	assert.False(t, m.policyMatchesBranch(config, "repo", "ref"))
}

func TestAdoGetPipelineLogs_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("List Path - Request Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		server.Close() // Force failure

		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})

	t.Run("List Path - Non-200 Status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
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

		result, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.NoError(t, err)
		assert.Equal(t, "No logs found for this run.", result.Text)
	})

	t.Run("Content Path - Request Failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		server.Close() // Force failure

		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1, "log_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})
}

func TestAdoGetPrStatuses_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("fetchPrStatuses - 404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoGetPrStatuses(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("fetchPrStatuses - 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoGetPrStatuses(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestAdoListBranchPolicies_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("fetchRepositoryId - 404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"})
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

		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode repository metadata")
	})
}

func TestPerformPolicyEvaluationRequest_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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

	m := NewADOManager(security.NewSecurityManager(nil), WithBaseURL(server.URL), WithToken("test-pat"))

	_, err := m.adoGetFileContent(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned status: 418")
}

func TestAdoListPipelineRuns_Empty(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value": []}`))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(security.NewSecurityManager(nil), WithBaseURL(server.URL), WithToken("test-pat"))

	result, err := m.adoListPipelineRuns(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1})
	assert.NoError(t, err)
	assert.Equal(t, "No pipeline runs found.", result.Text)
}

func TestAdoGetBuildTimeline_Detailed(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoGetBuildTimeline(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Default", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)

		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		_, err := m.adoGetBuildTimeline(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestAdoGetBuildChanges_Empty(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"value": []}`))
	}))
	t.Cleanup(server.Close)

	m := NewADOManager(security.NewSecurityManager(nil), WithBaseURL(server.URL), WithToken("test-pat"))

	result, err := m.adoGetBuildChanges(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
	assert.NoError(t, err)
	assert.Equal(t, "[]", result.Text)
}

func TestAdoTools_MissingParams(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)
	m := NewADOManager(sm)
	ctx := context.Background()

	t.Run("adoGetFileContent", func(t *testing.T) {
		_, err := m.adoGetFileContent(ctx, map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("adoGetBuildChanges", func(t *testing.T) {
		_, err := m.adoGetBuildChanges(ctx, map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("adoGetPrStatuses", func(t *testing.T) {
		_, err := m.adoGetPrStatuses(ctx, map[string]interface{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})
}

func TestAdoGetPullRequest_UnmarshalError(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	m := NewADOManager(nil)
	_, err := m.adoGetPullRequest(context.Background(), map[string]interface{}{"pull_request_id": "invalid"})
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
	sm := security.NewSecurityManager(nil)

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
		{"adoListPullRequests", func() (tools.ToolResult, error) { return m.adoListPullRequests(ctx, args) }},
		{"adoGetPullRequest", func() (tools.ToolResult, error) { return m.adoGetPullRequest(ctx, args) }},
		{"adoGetPrDiff", func() (tools.ToolResult, error) { return m.adoGetPrDiff(ctx, args) }},
		{"adoGetPrThreads", func() (tools.ToolResult, error) { return m.adoGetPrThreads(ctx, args) }},
		{"adoListRepositoryItems", func() (tools.ToolResult, error) { return m.adoListRepositoryItems(ctx, args) }},
		{"adoListPipelineRuns", func() (tools.ToolResult, error) { return m.adoListPipelineRuns(ctx, args) }},
		{"adoGetPipelineRun", func() (tools.ToolResult, error) { return m.adoGetPipelineRun(ctx, args) }},
		{"adoGetPipelineLogs", func() (tools.ToolResult, error) { return m.adoGetPipelineLogs(ctx, args) }},
		{"adoGetPrStatuses", func() (tools.ToolResult, error) { return m.adoGetPrStatuses(ctx, args) }},
		{"adoGetPrPolicyEvaluations", func() (tools.ToolResult, error) { return m.adoGetPrPolicyEvaluations(ctx, args) }},
		{"adoListBranchPolicies", func() (tools.ToolResult, error) { return m.adoListBranchPolicies(ctx, args) }},
		{"adoGetBuildTimeline", func() (tools.ToolResult, error) { return m.adoGetBuildTimeline(ctx, args) }},
		{"adoGetBuildChanges", func() (tools.ToolResult, error) { return m.adoGetBuildChanges(ctx, args) }},
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
	sm := security.NewSecurityManager(nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		handler      func(w http.ResponseWriter, r *http.Request)
		call         func(m *ADOManager) (tools.ToolResult, error)
		expectedText string
		expectedErr  string
	}{
		{
			name: "Success - GetPullRequest",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"title": "PR Title", "status": "active", "createdBy": {"displayName": "User"}, "creationDate": "2023-01-01", "repository": {"name": "repo"}}`))
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				})
			},
			expectedText: "PR Title",
		},
		{
			name: "Success - ListPipelineRuns",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"value": [{"id": 1, "buildNumber": "run1", "status": "completed", "result": "succeeded", "queueTime": "2023-10-01", "repository": {"name": "repo"}}]}`))
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				return m.adoListPipelineRuns(ctx, map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1})
			},
			expectedText: "Run ID: 1",
		},
		{
			name: "Error - 500 Internal Server Error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				})
			},
			expectedErr: "returned status: 500",
		},
		{
			name: "Error - 503 Service Unavailable",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("service unavailable"))
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				})
			},
			expectedErr: "returned status: 503",
		},
		{
			name: "Error - 504 Gateway Timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("gateway timeout"))
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				})
			},
			expectedErr: "returned status: 504",
		},
		{
			name: "Error - Context Cancellation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				childCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
				return m.adoGetPullRequest(childCtx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				})
			},
			expectedErr: "context deadline exceeded",
		},
		{
			name: "Error - Malformed JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{ malformed`))
			},
			call: func(m *ADOManager) (tools.ToolResult, error) {
				return m.adoGetPullRequest(ctx, map[string]interface{}{
					"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
				})
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

func TestAdoListPipelineRuns_Features(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

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
		result, err := m.adoListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Recent runs for pipeline 42")
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
		result, err := m.adoListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Run ID: 102")
		assert.NotContains(t, result.Text, "Run ID: 101")
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
		result, err := m.adoListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Run ID: 101")
		assert.Contains(t, result.Text, "Run ID: 102")
		assert.NotContains(t, result.Text, "Run ID: 103")
	})
}

func TestAdoCreatePipeline(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	tests := []struct {
		name              string
		approved          bool
		existingPipelines []adoPipeline
		createResponse    string
		expectedText      string
		expectPost        bool
		expectConfirm     bool
	}{
		{
			name:     "Success",
			approved: true,
			existingPipelines: []adoPipeline{
				{Id: 1, Name: "Other"},
			},
			createResponse: `{"id": 123}`,
			expectedText:   "Successfully created pipeline 'NewPipeline' with ID: 123",
			expectPost:     true,
			expectConfirm:  true,
		},
		{
			name:     "Idempotency",
			approved: false,
			existingPipelines: []adoPipeline{
				{Id: 1, Name: "NewPipeline"},
			},
			expectedText:  "Pipeline 'NewPipeline' already exists with ID: 1",
			expectPost:    false,
			expectConfirm: false,
		},
		{
			name:     "Cancellation",
			approved: false,
			existingPipelines: []adoPipeline{
				{Id: 1, Name: "Other"},
			},
			expectedText:  "Pipeline creation cancelled by user.",
			expectPost:    false,
			expectConfirm: true,
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

			sm := &mockSecurityManager{approved: tt.approved}
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

			result, err := m.adoCreatePipeline(context.Background(), args)
			assert.NoError(t, err)
			assert.Contains(t, result.Text, tt.expectedText)
			assert.Equal(t, tt.expectPost, postCalled, "POST call mismatch")
			assert.Equal(t, tt.expectConfirm, sm.confirmCalled, "Confirm call mismatch")

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

		sm := &mockSecurityManager{approved: true}
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))

		args := map[string]interface{}{
			"organization":        "myorg",
			"project":             "myproj",
			"pipeline_id":         1,
			"branch":              "feature",
			"variables":           map[string]string{"var1": "val1"},
			"template_parameters": map[string]string{"param1": "paramVal"},
		}

		result, err := m.adoRunPipeline(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Successfully triggered pipeline run ID: 101")
		assert.Contains(t, result.Text, "Web URL: https://dev.azure.com/myorg/myproj/_build/results?buildId=101")
	})

	t.Run("Cancelled", func(t *testing.T) {
		sm := &mockSecurityManager{approved: false}
		m := NewADOManager(sm)

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"pipeline_id":  1,
		}

		result, err := m.adoRunPipeline(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, "Pipeline run cancelled by user.", result.Text)
	})
}

type mockSecurityManager struct {
	approved      bool
	err           error
	confirmCalled bool
}

func (m *mockSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (m *mockSecurityManager) IsPathWritable(path string) (string, error) {
	return path, nil
}
func (m *mockSecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return m.approved, m.err
}
func (m *mockSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return m.approved, m.err
}
func (m *mockSecurityManager) LogAudit(label1, val1, label2, val2 string) {}
func (m *mockSecurityManager) TerminalLock()                              {}
func (m *mockSecurityManager) TerminalUnlock()                            {}
func (m *mockSecurityManager) Prompt(message string)                      {}
func (m *mockSecurityManager) Warn(message string)                        {}
func (m *mockSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	m.confirmCalled = true
	return m.approved, m.err
}
func (m *mockSecurityManager) ReadLine(ctx context.Context) (string, error) { return "", nil }
func (m *mockSecurityManager) IsCommandAllowed(command string) bool         { return true }
func (m *mockSecurityManager) IsBypassActive() bool                         { return false }
