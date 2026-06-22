// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

func TestThreadFilter(t *testing.T) {
	// Helper to build a thread inline
	makeThread := func(deleted bool, commentTypes ...string) adoThread {
		var comments []struct {
			Author struct {
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Content       string `json:"content"`
			PublishedDate string `json:"publishedDate"`
			CommentType   string `json:"commentType"`
		}
		for _, ct := range commentTypes {
			comments = append(comments, struct {
				Author struct {
					DisplayName string `json:"displayName"`
				} `json:"author"`
				Content       string `json:"content"`
				PublishedDate string `json:"publishedDate"`
				CommentType   string `json:"commentType"`
			}{CommentType: ct, Content: "test", Author: struct {
				DisplayName string `json:"displayName"`
			}{DisplayName: "User"}, PublishedDate: "2023-01-01"})
		}
		return adoThread{Comments: comments, IsDeleted: deleted}
	}

	tests := []struct {
		name   string
		thread adoThread
		want   bool
	}{
		{"user comments only", makeThread(false, "text", "text"), true},
		{"mixed system and user", makeThread(false, "system", "text"), true},
		{"single user comment", makeThread(false, "text"), true},
		{"deleted thread", makeThread(true, "text"), false},
		{"all system comments", makeThread(false, "system", "system"), false},
		{"single system comment", makeThread(false, "system"), false},
		{"no comments", makeThread(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, threadFilter(tt.thread))
		})
	}
}

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

	t.Run("ZeroValueParams", func(t *testing.T) {
		m := NewADOManager(sm)
		result, err := m.adoGetPullRequest(context.Background(), map[string]interface{}{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
		assert.Equal(t, tools.ToolResult{}, result)
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

func TestAdoGetPullRequest_Validation(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name          string
		args          map[string]interface{}
		errorContains string
	}{
		{
			name:          "Missing organization",
			args:          map[string]interface{}{"project": "p", "repository": "r", "pull_request_id": 1},
			errorContains: "required",
		},
		{
			name:          "Missing project",
			args:          map[string]interface{}{"organization": "o", "repository": "r", "pull_request_id": 1},
			errorContains: "required",
		},
		{
			name:          "Missing repository",
			args:          map[string]interface{}{"organization": "o", "project": "p", "pull_request_id": 1},
			errorContains: "required",
		},
		{
			name:          "Zero pull_request_id",
			args:          map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 0},
			errorContains: "required",
		},
		{
			name:          "All empty",
			args:          map[string]interface{}{},
			errorContains: "required",
		},
		{
			name:          "Empty organization",
			args:          map[string]interface{}{"organization": "", "project": "p", "repository": "r", "pull_request_id": 1},
			errorContains: "required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewADOManager(sm)

			result, err := m.adoGetPullRequest(context.Background(), tt.args, nil)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
			assert.Equal(t, tools.ToolResult{}, result)
		})
	}
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

	t.Run("Top Clamped To 1000", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			assert.Equal(t, "1000", q.Get("$top")) // clamped, not 2000
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value": []}`))
		}))
		t.Cleanup(server.Close)
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		_, err := m.AdoListPullRequests(context.Background(), map[string]interface{}{
			"organization": "myorg", "project": "myproj", "repository": "myrepo", "top": 2000,
		}, nil)
		assert.NoError(t, err)
	})

	t.Run("Malformed JSON Response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid`))
		}))
		t.Cleanup(server.Close)
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		_, err := m.AdoListPullRequests(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": "r",
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decoding response")
	})

	t.Run("Forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		t.Cleanup(server.Close)
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		_, err := m.AdoListPullRequests(context.Background(), map[string]interface{}{
			"organization": "myorg", "project": "myproj", "repository": "myrepo",
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "forbidden")
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

	t.Run("All System Threads", func(t *testing.T) {
		t.Parallel()
		m := &AdoManager{}
		threadData := adoThreadResponse{
			Value: []adoThread{
				{
					IsDeleted: false,
					Comments: []struct {
						Author struct {
							DisplayName string `json:"displayName"`
						} `json:"author"`
						Content       string `json:"content"`
						PublishedDate string `json:"publishedDate"`
						CommentType   string `json:"commentType"`
					}{
						{Author: struct {
							DisplayName string `json:"displayName"`
						}{DisplayName: "System"}, Content: "Build succeeded.", CommentType: "system"},
					},
				},
				{
					IsDeleted: true,
					Comments:  nil,
				},
			},
		}
		result := m.formatPrThreads(123, threadData)
		assert.Equal(t, "No discussion threads found in this pull request.", result)
	})

	t.Run("Empty Comment Content", func(t *testing.T) {
		t.Parallel()
		m := &AdoManager{}
		threadData := adoThreadResponse{
			Value: []adoThread{
				{
					IsDeleted: false,
					Comments: []struct {
						Author struct {
							DisplayName string `json:"displayName"`
						} `json:"author"`
						Content       string `json:"content"`
						PublishedDate string `json:"publishedDate"`
						CommentType   string `json:"commentType"`
					}{
						{Author: struct {
							DisplayName string `json:"displayName"`
						}{DisplayName: "Bot"}, Content: "", CommentType: "text"},
						{Author: struct {
							DisplayName string `json:"displayName"`
						}{DisplayName: "User"}, Content: "Real comment.", CommentType: "text"},
					},
				},
			},
		}
		result := m.formatPrThreads(123, threadData)
		assert.Contains(t, result, "Real comment.")
		assert.NotContains(t, result, "Bot:") // empty content comment is skipped
	})

	t.Run("Malformed JSON Response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid`))
		}))
		t.Cleanup(server.Close)
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		_, err := m.AdoGetPrThreads(context.Background(), map[string]interface{}{
			"organization": "o", "project": "p", "repository": "r", "pull_request_id": 123,
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decoding response")
	})

	t.Run("Unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		_, err := m.AdoGetPrThreads(context.Background(), map[string]interface{}{
			"organization": "myorg", "project": "myproj", "repository": "myrepo", "pull_request_id": 123,
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("Forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		t.Cleanup(server.Close)
		m := NewADOManager(sm, WithBaseURL(server.URL), WithToken("test-pat"))
		_, err := m.AdoGetPrThreads(context.Background(), map[string]interface{}{
			"organization": "myorg", "project": "myproj", "repository": "myrepo", "pull_request_id": 123,
		}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "forbidden")
	})
}

func TestAdoGetPrThreads_Validation(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	tests := []struct {
		name          string
		args          map[string]interface{}
		errorContains string
	}{
		{
			name:          "Missing organization",
			args:          map[string]interface{}{"project": "p", "repository": "r", "pull_request_id": 1},
			errorContains: "required",
		},
		{
			name:          "Missing project",
			args:          map[string]interface{}{"organization": "o", "repository": "r", "pull_request_id": 1},
			errorContains: "required",
		},
		{
			name:          "Missing repository",
			args:          map[string]interface{}{"organization": "o", "project": "p", "pull_request_id": 1},
			errorContains: "required",
		},
		{
			name:          "Zero pull_request_id",
			args:          map[string]interface{}{"organization": "o", "project": "p", "repository": "r", "pull_request_id": 0},
			errorContains: "required",
		},
		{
			name:          "All empty",
			args:          map[string]interface{}{},
			errorContains: "required",
		},
		{
			name:          "Empty organization",
			args:          map[string]interface{}{"organization": "", "project": "p", "repository": "r", "pull_request_id": 1},
			errorContains: "required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewADOManager(sm)

			result, err := m.AdoGetPrThreads(context.Background(), tt.args, nil)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
			assert.Equal(t, tools.ToolResult{}, result)
		})
	}
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
		assert.Contains(t, err.Error(), "decoding response")
	})
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
