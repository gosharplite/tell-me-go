// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockAzureDevOpsClient struct {
	mock.Mock
}

func (m *mockAzureDevOpsClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestAdoGetPullRequest(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

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

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(":test-pat"))
			expectedURL := "https://dev.azure.com/myorg/myproj/_apis/git/repositories/myrepo/pullrequests/123?api-version=7.1"
			return req.URL.String() == expectedURL && req.Header.Get("Authorization") == expectedAuth
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

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
		mockClient.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

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
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		_, err := m.adoGetPullRequest(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pull request not found")
	})

	t.Run("Missing Parameters", func(t *testing.T) {
		m := NewAzureDevOpsManager(sm, nil)
		args := map[string]interface{}{
			"organization": "myorg",
		}

		_, err := m.adoGetPullRequest(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Missing PAT", func(t *testing.T) {
		t.Setenv("AZURE_PAT_ALL", "")
		m := NewAzureDevOpsManager(sm, nil)
		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 123,
		}

		_, err := m.adoGetPullRequest(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing AZURE_PAT_ALL")
	})
}

func TestAdoListPullRequests(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

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

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			q := req.URL.Query()
			return strings.Contains(req.URL.String(), "/pullrequests") &&
				q.Get("searchCriteria.status") == "active" &&
				q.Get("$top") == "50"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

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
		mockClient.AssertExpectations(t)
	})

	t.Run("Empty Results", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		jsonResponse := `{"value": [], "count": 0}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

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
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			q := req.URL.Query()
			return q.Get("searchCriteria.status") == "completed" && q.Get("$top") == "10"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
		}, nil)

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"status":       "completed",
			"top":          10,
		}

		_, err := m.adoListPullRequests(context.Background(), args)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})
}

func TestAdoGetPrDiff(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

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

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pullrequests/123/iterations/1/changes")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

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
		mockClient.AssertExpectations(t)
	})

	t.Run("No Changes", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"changeEntries": []}`)),
		}, nil)

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
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

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

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pullrequests/123/threads")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

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
		mockClient.AssertExpectations(t)
	})

	t.Run("No Threads", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
		}, nil)

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
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		fileContent := "package main\n\nfunc main() {}"

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			q := req.URL.Query()
			return strings.Contains(req.URL.String(), "/items") &&
				q.Get("path") == "/src/main.go" &&
				q.Get("versionDescriptor.version") == "develop" &&
				q.Get("$format") == "text"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(fileContent)),
		}, nil)

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
		mockClient.AssertExpectations(t)
	})

	t.Run("Default Version", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.URL.Query().Get("versionDescriptor.version") == "main"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("content")),
		}, nil)

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"path":         "/src/main.go",
		}

		_, err := m.adoGetFileContent(context.Background(), args)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("File Not Found", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"organization": "myorg",
			"project":      "myproj",
			"repository":   "myrepo",
			"path":         "/missing.go",
		}

		_, err := m.adoGetFileContent(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file not found")
	})
}

func TestAdoListRepositoryItems(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	sm := security.NewSecurityManager(nil)

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		jsonResponse := `{
			"value": [
				{"path": "/src", "isFolder": true},
				{"path": "/src/main.go", "isFolder": false},
				{"path": "/README.md", "isFolder": false}
			],
			"count": 3
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			q := req.URL.Query()
			return strings.Contains(req.URL.String(), "/items") &&
				q.Get("scopePath") == "/" &&
				q.Get("recursionLevel") == "oneLevel"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

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
		mockClient.AssertExpectations(t)
	})

	t.Run("Empty Results", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
		}, nil)

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
	mockClient := new(mockAzureDevOpsClient)
	m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{"value": [{"id": 101, "name": "run1", "state": "completed", "result": "succeeded", "createdDate": "2023-10-01"}]}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pipelines/1/runs")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1}
		result, err := m.adoListPipelineRuns(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Run ID: 101")
	})
}

func TestAdoGetPipelineRun(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

	t.Run("Success", func(t *testing.T) {
		jsonResponse := `{"id": 101, "name": "run1", "state": "completed", "result": "succeeded", "createdDate": "2023-10-01", "url": "http://run"}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pipelines/1/runs/101")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		result, err := m.adoGetPipelineRun(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Pipeline Run #101 Details")
	})
}

func TestAdoGetPipelineLogs(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

	t.Run("List Logs", func(t *testing.T) {
		jsonResponse := `{"value": [{"id": 1, "lineCount": 10}]}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/runs/101/logs") && !strings.Contains(req.URL.String(), "/logs/1")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101}
		result, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Log ID: 1")
	})

	t.Run("Fetch Log Content", func(t *testing.T) {
		logContent := "build output"
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/runs/101/logs/1")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(logContent)),
		}, nil).Once()

		args := map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 101, "log_id": 1}
		result, err := m.adoGetPipelineLogs(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, logContent, result.Text)
	})
}

func TestAdoGetPrStatuses(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pullrequests/123/statuses")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

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
		mockClient.AssertExpectations(t)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil).Once()

		args := map[string]interface{}{
			"organization":    "myorg",
			"project":         "myproj",
			"repository":      "myrepo",
			"pull_request_id": 999,
		}

		_, err := m.adoGetPrStatuses(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pull request or repository not found")
	})
}

func TestAdoGetPrPolicyEvaluations(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

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
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

		// Mock PR metadata lookup to get project ID
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/git/repositories/myrepo/pullrequests/123")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"repository": {"project": {"id": "proj-guid"}}}`)),
		}, nil).Once()

		// Mock policy evaluations lookup using the CodeReview artifact ID
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/myorg/myproj/_apis/policy/evaluations") &&
				req.URL.Query().Get("artifactId") == "vstfs:///CodeReview/CodeReviewId/proj-guid/123" &&
				req.URL.Query().Get("api-version") == "7.1-preview.1"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

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
		mockClient.AssertExpectations(t)
	})

	t.Run("Empty", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/git/repositories/myrepo/pullrequests/123")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"repository": {"project": {"id": "proj-guid"}}}`)),
		}, nil).Once()

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/policy/evaluations")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
		}, nil).Once()

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
		mockClient := new(mockAzureDevOpsClient)
		m := NewAzureDevOpsManager(security.NewSecurityManager(nil), mockClient)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/git/repositories/myrepo/pullrequests/123")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"repository": {"project": {"id": "proj-guid"}}}`)),
		}, nil).Once()

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil).Once()

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
