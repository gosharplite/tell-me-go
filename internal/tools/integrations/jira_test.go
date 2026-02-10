// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockJiraClient struct {
	mock.Mock
}

func (m *mockJiraClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestJiraManager_JiraSearchIssues(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockJiraClient)
		m := NewJiraManager(nil, mockClient)

		jsonResponse := `{
			"total": 1,
			"issues": [
				{
					"key": "PROJ-1",
					"fields": {
						"summary": "Test Issue",
						"status": { "name": "In Progress" }
					}
				}
			]
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/rest/api/3/search") && req.URL.Query().Get("jql") == "project = PROJ"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"jql": "project = PROJ"}
		result, err := m.jiraSearchIssues(context.Background(), args)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Found 1 issues (showing 1):")
		assert.Contains(t, result.Text, "[PROJ-1] Test Issue (Status: In Progress, Assignee: Unassigned)")
		mockClient.AssertExpectations(t)
	})

	t.Run("No Issues Found", func(t *testing.T) {
		mockClient := new(mockJiraClient)
		m := NewJiraManager(nil, mockClient)

		jsonResponse := `{"total": 0, "issues": []}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"jql": "project = NONE"}
		result, err := m.jiraSearchIssues(context.Background(), args)

		assert.NoError(t, err)
		assert.Equal(t, "No issues found matching the JQL query.", result.Text)
	})

	t.Run("API Error", func(t *testing.T) {
		mockClient := new(mockJiraClient)
		m := NewJiraManager(nil, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader("Invalid JQL")),
		}, nil)

		args := map[string]interface{}{"jql": "invalid"}
		_, err := m.jiraSearchIssues(context.Background(), args)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jira API returned status: 400")
	})
}

func TestJiraManager_JiraGetIssue(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("Success with Description", func(t *testing.T) {
		mockClient := new(mockJiraClient)
		m := NewJiraManager(nil, mockClient)

		jsonResponse := `{
			"key": "PROJ-1",
			"fields": {
				"summary": "Detailed Issue",
				"status": { "name": "Open" },
				"priority": { "name": "High" },
				"assignee": { "displayName": "Alice Smith" },
				"description": {
					"type": "doc",
					"version": 1,
					"content": [
						{
							"type": "paragraph",
							"content": [
								{ "type": "text", "text": "This is a " },
								{ "type": "text", "text": "cool description." }
							]
						}
					]
				}
			}
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/rest/api/3/issue/PROJ-1")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"issue_key": "PROJ-1"}
		result, err := m.jiraGetIssue(context.Background(), args)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "# [PROJ-1] Detailed Issue")
		assert.Contains(t, result.Text, "**Status**: Open")
		assert.Contains(t, result.Text, "**Priority**: High")
		assert.Contains(t, result.Text, "**Assignee**: Alice Smith")
		assert.Contains(t, result.Text, "This is a cool description.")
		mockClient.AssertExpectations(t)
	})

	t.Run("Success No Assignee No Description", func(t *testing.T) {
		mockClient := new(mockJiraClient)
		m := NewJiraManager(nil, mockClient)

		jsonResponse := `{
			"key": "PROJ-2",
			"fields": {
				"summary": "Empty Issue",
				"status": { "name": "To Do" },
				"priority": { "name": "Low" },
				"assignee": null,
				"description": null
			}
		}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"issue_key": "PROJ-2"}
		result, err := m.jiraGetIssue(context.Background(), args)

		assert.NoError(t, err)
		assert.Contains(t, result.Text, "**Assignee**: Unassigned")
		assert.Contains(t, result.Text, "No description provided.")
	})

	t.Run("Issue Not Found", func(t *testing.T) {
		mockClient := new(mockJiraClient)
		m := NewJiraManager(nil, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{"issue_key": "MISSING-1"}
		_, err := m.jiraGetIssue(context.Background(), args)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jira issue not found: MISSING-1")
	})
}

func TestJiraManager_ParseADF(t *testing.T) {
	m := NewJiraManager(nil, nil)

	t.Run("Complex ADF", func(t *testing.T) {
		adf := map[string]interface{}{
			"type": "doc",
			"content": []interface{}{
				map[string]interface{}{
					"type": "heading",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "Heading"},
					},
				},
				map[string]interface{}{
					"type": "paragraph",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "First line."},
					},
				},
				map[string]interface{}{
					"type": "paragraph",
					"content": []interface{}{
						map[string]interface{}{"type": "text", "text": "Second line with "},
						map[string]interface{}{"type": "text", "text": "formatting."},
					},
				},
			},
		}

		result := m.parseADF(adf)
		expected := "Heading\nFirst line.\nSecond line with formatting."
		assert.Equal(t, expected, result)
	})

	t.Run("Empty ADF", func(t *testing.T) {
		assert.Equal(t, "", m.parseADF(nil))
	})
}
