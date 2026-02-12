// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Missing Parameters", func(t *testing.T) {
		m := newazureDevOpsManager(sm, nil)
		args := map[string]interface{}{
			"organization": "myorg",
		}

		_, err := m.adoGetPullRequest(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("Missing PAT", func(t *testing.T) {
		t.Setenv("AZURE_PAT_ALL", "")
		m := newazureDevOpsManager(sm, nil)
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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil)

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
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)

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
		m := newazureDevOpsManager(sm, mockClient)

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
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
			Status:     "404 Not Found",
		}, nil).Once()

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
		m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
		m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
		m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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

func TestAdoListBranchPolicies(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

		// Mock repository lookup
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/git/repositories/myrepo")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "repo-guid"}`)),
		}, nil).Once()

		// Mock policy configurations lookup
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
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/policy/configurations")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(policyResponse)),
		}, nil).Once()

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
		mockClient.AssertExpectations(t)
	})

	t.Run("No Policies", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/git/repositories/myrepo")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "repo-guid"}`)),
		}, nil).Once()

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/policy/configurations")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
		}, nil).Once()

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
	mockClient := new(mockAzureDevOpsClient)
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/build/builds/123/timeline")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123}
		result, err := m.adoGetBuildTimeline(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, `"name": "Task 1"`)
		assert.Contains(t, result.Text, `"log": {`)
		assert.Contains(t, result.Text, `"id": 10`)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/build/builds/123/timeline")
		})).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123}
		_, err := m.adoGetBuildTimeline(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetTaskLog(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

	t.Run("Success", func(t *testing.T) {
		logContent := "Successfully completed task"
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/build/builds/123/logs/10")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(logContent)),
		}, nil).Once()

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
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()

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
	mockClient := new(mockAzureDevOpsClient)
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)

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
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/build/builds/123/changes") && req.URL.Query().Get("$top") == "10"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 123, "top": 10}
		result, err := m.adoGetBuildChanges(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, `"id": "abc123"`)
		assert.Contains(t, result.Text, `"message": "feat: add something"`)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()

		args := map[string]interface{}{"organization": "o", "project": "p", "build_id": 999}
		_, err := m.adoGetBuildChanges(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoTools_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	commonArgs := map[string]interface{}{
		"organization": "myorg",
		"project":      "myproj",
		"repository":   "myrepo",
	}

	tests := []struct {
		name           string
		toolFunc       func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
		args           map[string]interface{}
		httpStatus     int
		respBody       string
		doErr          error
		expectedErrMsg string
		setupPAT       string
	}{
		{
			name: "adoGetPrDiff - Unmarshal Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"pull_request_id": "invalid"}, // should be int
			expectedErrMsg: "json: cannot unmarshal string into Go struct field",
		},
		{
			name: "adoGetPrDiff - Missing Params",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "adoGetPrDiff - Request Failure",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			doErr:          fmt.Errorf("network error"),
			expectedErrMsg: "request failed",
		},
		{
			name: "adoGetPrDiff - 401 Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrDiff - 403 Forbidden",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrDiff - 404 Not Found",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoGetPrDiff - 500 Internal Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrDiff(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusInternalServerError,
			respBody:       "internal error",
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "adoGetTaskLog - Unmarshal Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"build_id": "invalid"},
			expectedErrMsg: "json: cannot unmarshal string into Go struct field",
		},
		{
			name: "adoGetTaskLog - Missing Params",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "adoGetTaskLog - 404 Not Found",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "log_id": 1},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "not found",
		},
		{
			name: "adoGetBuildTimeline - Missing Params",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildTimeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o"},
			expectedErrMsg: "required",
		},
		{
			name: "adoGetBuildTimeline - 401 Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildTimeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrPolicyEvaluations - PR Metadata Failure",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPullRequests(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoListPullRequests - Forbidden",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPullRequests(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoListPullRequests - Not Found",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPullRequests(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoGetPrThreads - Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrThreads(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrThreads - Forbidden",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrThreads(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrThreads - Not Found",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrThreads(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoListRepositoryItems - Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListRepositoryItems(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoListRepositoryItems - Forbidden",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListRepositoryItems(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoListRepositoryItems - Not Found",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListRepositoryItems(ctx, args)
			},
			args:           commonArgs,
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
		{
			name: "adoListPipelineRuns - 500 Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoListPipelineRuns(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "adoGetPipelineRun - 500 Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPipelineRun(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "adoGetBuildChanges - 500 Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildChanges(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "status: 500",
		},
		{
			name: "adoGetBuildChanges - 401 Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildChanges(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrStatuses - 401 Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrStatuses(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetPrStatuses - 403 Forbidden",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrStatuses(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrPolicyEvaluations - 403 Forbidden",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrPolicyEvaluations(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusForbidden,
			expectedErrMsg: "forbidden",
		},
		{
			name: "adoGetPrPolicyEvaluations - PR Metadata Decode Failure",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPrPolicyEvaluations(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1},
			httpStatus:     http.StatusOK,
			respBody:       `{invalid}`,
			expectedErrMsg: "failed to decode PR metadata",
		},
		{
			name: "adoGetFileContent - Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetFileContent(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetFileContent - Default Error",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetFileContent(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"},
			httpStatus:     http.StatusInternalServerError,
			expectedErrMsg: "returned status: 500",
		},
		{
			name: "adoGetPipelineLogs - Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetPipelineLogs(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetBuildTimeline - Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildTimeline(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetTaskLog - Unauthorized",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetTaskLog(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1, "log_id": 1},
			httpStatus:     http.StatusUnauthorized,
			expectedErrMsg: "unauthorized",
		},
		{
			name: "adoGetBuildChanges - Not Found",
			toolFunc: func(m *azureDevOpsManager, ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
				return m.adoGetBuildChanges(ctx, args)
			},
			args:           map[string]interface{}{"organization": "o", "project": "p", "build_id": 1},
			httpStatus:     http.StatusNotFound,
			expectedErrMsg: "resource not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupPAT != "" {
				t.Setenv("AZURE_PAT_ALL", tt.setupPAT)
			} else {
				t.Setenv("AZURE_PAT_ALL", "test-pat")
			}

			mockClient := new(mockAzureDevOpsClient)
			m := newazureDevOpsManager(sm, mockClient)

			if tt.doErr != nil {
				mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), tt.doErr)
			} else if tt.respBody != "" || tt.httpStatus != 0 {
				resp := &http.Response{
					StatusCode: tt.httpStatus,
					Body:       io.NopCloser(strings.NewReader(tt.respBody)),
					Status:     fmt.Sprintf("%d %s", tt.httpStatus, http.StatusText(tt.httpStatus)),
				}
				mockClient.On("Do", mock.Anything).Return(resp, nil)
			}

			_, err := tt.toolFunc(m, context.Background(), tt.args)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErrMsg)
		})
	}

	t.Run("adoGetPrPolicyEvaluations - Policy List Failure", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)

		// First call succeeds
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pullrequests/123")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"repository": {"project": {"id": "proj-guid"}}}`)),
		}, nil).Once()

		// Second call fails
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/policy/evaluations")
		})).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("internal error")),
			Status:     "500 Internal Server Error",
		}, nil).Once()

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
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)

		// First call succeeds
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/git/repositories/myrepo")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "repo-guid"}`)),
		}, nil).Once()

		// Second call fails
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/_apis/policy/configurations")
		})).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("internal error")),
			Status:     "500 Internal Server Error",
		}, nil).Once()

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
	t.Setenv("AZURE_PAT_ALL", "")
	sm := security.NewSecurityManager(nil)
	m := newazureDevOpsManager(sm, nil)
	ctx := context.Background()
	args := map[string]interface{}{
		"organization": "o",
		"project":      "p",
		"repository":   "r",
	}

	_, err := m.adoListPullRequests(ctx, args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing AZURE_PAT_ALL")
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
	m := &azureDevOpsManager{}
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
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), fmt.Errorf("fail")).Once()
		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})

	t.Run("List Path - Non-200 Status", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("err")),
			Status:     "500 Internal Error",
		}, nil).Once()
		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})

	t.Run("List Path - Empty Logs", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
		}, nil).Once()
		result, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1})
		assert.NoError(t, err)
		assert.Equal(t, "No logs found for this run.", result.Text)
	})

	t.Run("Content Path - Request Failure", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), fmt.Errorf("fail")).Once()
		_, err := m.adoGetPipelineLogs(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1, "run_id": 1, "log_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})
}

func TestAdoGetPrStatuses_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("fetchPrStatuses - 404", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()
		_, err := m.adoGetPrStatuses(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("fetchPrStatuses - 500", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("err")),
			Status:     "500 Internal Error",
		}, nil).Once()
		_, err := m.adoGetPrStatuses(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestAdoListBranchPolicies_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("fetchRepositoryId - 404", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()
		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("fetchRepositoryId - Decode Error", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{invalid}`)),
		}, nil).Once()
		_, err := m.adoListBranchPolicies(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "branch_name": "b"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode repository metadata")
	})
}

func TestPerformPolicyEvaluationRequest_DetailedErrors(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("401", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "401 Unauthorized",
		}, nil).Once()
		_, err := m.performPolicyEvaluationRequest(context.Background(), "http://url")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("404", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()
		_, err := m.performPolicyEvaluationRequest(context.Background(), "http://url")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})
}

func TestAdoGetFileContent_DefaultStatus(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)
	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusTeapot,
		Body:       io.NopCloser(strings.NewReader("teapot")),
		Status:     "418 I'm a teapot",
	}, nil).Once()
	_, err := m.adoGetFileContent(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "path": "f"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "returned status: 418")
}

func TestAdoListPipelineRuns_Empty(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)
	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
	}, nil).Once()
	result, err := m.adoListPipelineRuns(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "pipeline_id": 1})
	assert.NoError(t, err)
	assert.Equal(t, "No pipeline runs found.", result.Text)
}

func TestAdoGetBuildTimeline_Detailed(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)

	t.Run("404", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "404 Not Found",
		}, nil).Once()
		_, err := m.adoGetBuildTimeline(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource not found")
	})

	t.Run("Default", func(t *testing.T) {
		mockClient := new(mockAzureDevOpsClient)
		m := newazureDevOpsManager(sm, mockClient)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("")),
			Status:     "500 Internal Error",
		}, nil).Once()
		_, err := m.adoGetBuildTimeline(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "returned status: 500")
	})
}

func TestAdoGetBuildChanges_Empty(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	mockClient := new(mockAzureDevOpsClient)
	m := newazureDevOpsManager(security.NewSecurityManager(nil), mockClient)
	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"value": []}`)),
	}, nil).Once()
	result, err := m.adoGetBuildChanges(context.Background(), map[string]interface{}{"organization": "o", "project": "p", "build_id": 1})
	assert.NoError(t, err)
	assert.Equal(t, "[]", result.Text)
}

func TestAdoTools_MissingParams(t *testing.T) {
	t.Setenv("AZURE_PAT_ALL", "test-pat")
	sm := security.NewSecurityManager(nil)
	m := newazureDevOpsManager(sm, nil)
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
	m := newazureDevOpsManager(nil, nil)
	_, err := m.adoGetPullRequest(context.Background(), map[string]interface{}{"pull_request_id": "invalid"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}
