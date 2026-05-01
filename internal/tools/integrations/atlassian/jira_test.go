// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockJiraClient struct {
	mock.Mock
}

func (m *mockJiraClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestJiraManager_JiraSearchIssues(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	tests := []struct {
		name          string
		args          map[string]interface{}
		mockResp      *http.Response
		mockErr       error
		ctx           context.Context
		expectedError string
		expectedText  string
	}{
		{
			name: "Success",
			args: map[string]interface{}{"jql": "project = PROJ"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"total": 1,
					"issues": [
						{
							"key": "PROJ-1",
							"fields": {
								"summary": "Test Issue",
								"status": { "name": "In Progress" },
								"assignee": { "displayName": "Bob" }
							}
						}
					]
				}`)),
			},
			expectedText: "[PROJ-1] Test Issue (Status: In Progress, Assignee: Bob)",
		},
		{
			name: "Success with limit",
			args: map[string]interface{}{"jql": "project = PROJ", "limit": 2000}, // Should be capped at 1000
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"total": 0, "issues": []}`)),
			},
			expectedText: "No issues found matching the JQL query.",
		},
		{
			name: "No Issues Found",
			args: map[string]interface{}{"jql": "project = NONE"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"total": 0, "issues": []}`)),
			},
			expectedText: "No issues found matching the JQL query.",
		},
		{
			name:          "Unmarshal Args Error",
			args:          map[string]interface{}{"limit": "not-an-int"},
			expectedError: "cannot unmarshal string into Go struct field .limit of type int",
		},
		{
			name:          "Missing JQL",
			args:          map[string]interface{}{},
			expectedError: "jql argument is required",
		},
		{
			name: "API Error 400",
			args: map[string]interface{}{"jql": "invalid"},
			mockResp: &http.Response{
				StatusCode: http.StatusBadRequest,
				Status:     "400 Bad Request",
				Body:       io.NopCloser(strings.NewReader("Invalid JQL")),
			},
			expectedError: "jira API returned status: 400 Bad Request, body: Invalid JQL",
		},
		{
			name: "API Error 500",
			args: map[string]interface{}{"jql": "project = PROJ"},
			mockResp: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Body:       io.NopCloser(strings.NewReader("Internal Error")),
			},
			expectedError: "jira API returned status: 500 Internal Server Error, body: Internal Error",
		},
		{
			name: "Malformed JSON",
			args: map[string]interface{}{"jql": "project = PROJ"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{ "issues": [ { "key": `)),
			},
			expectedError: "failed to decode response",
		},
		{
			name: "Success Total Found Zero",
			args: map[string]interface{}{"jql": "project = PROJ"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"total": 0,
					"issues": [
						{
							"key": "PROJ-1",
							"fields": {
								"summary": "Test Issue",
								"status": { "name": "In Progress" },
								"assignee": { "displayName": "Bob" }
							}
						}
					]
				}`)),
			},
			expectedText: "Found 1 issues (showing 1):",
		},
		{
			name: "Context Timeout",
			args: map[string]interface{}{"jql": "project = PROJ"},
			mockErr: func() error {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
				defer cancel()
				return ctx.Err()
			}(),
			expectedError: "context deadline exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(mockJiraClient)
			m, err := newjiraManager(nil, mockClient)
			assert.NoError(t, err)
			m.provider.baseDelay = 1 * time.Microsecond

			if tt.mockResp != nil || tt.mockErr != nil {
				mockClient.On("Do", mock.Anything).Return(tt.mockResp, tt.mockErr)
			}

			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}

			result, err := m.jiraSearchIssues(ctx, tt.args, nil)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Contains(t, result.Text, tt.expectedText)
			}
		})
	}
}

func TestJiraManager_JiraGetIssue(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	tests := []struct {
		name          string
		args          map[string]interface{}
		mockResp      *http.Response
		mockErr       error
		expectedError string
		expectedText  string
	}{
		{
			name: "Success with Description",
			args: map[string]interface{}{"issue_key": "PROJ-1"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
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
				}`)),
			},
			expectedText: "This is a cool description.",
		},
		{
			name: "Success No Assignee No Description",
			args: map[string]interface{}{"issue_key": "PROJ-2"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"key": "PROJ-2",
					"fields": {
						"summary": "Empty Issue",
						"status": { "name": "To Do" },
						"priority": { "name": "Low" },
						"assignee": null,
						"description": null
					}
				}`)),
			},
			expectedText: "**Assignee**: Unassigned",
		},
		{
			name:          "Issue Not Found",
			args:          map[string]interface{}{"issue_key": "MISSING-1"},
			mockResp:      &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))},
			expectedError: "jira issue not found: MISSING-1",
		},
		{
			name:          "Unmarshal Args Error",
			args:          map[string]interface{}{"issue_key": 123},
			expectedError: "cannot unmarshal number into Go struct field .issue_key of type string",
		},
		{
			name:          "Missing issue_key",
			args:          map[string]interface{}{},
			expectedError: "issue_key argument is required",
		},
		{
			name: "Malformed JSON",
			args: map[string]interface{}{"issue_key": "PROJ-1"},
			mockResp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{ "key": "PROJ-1", "fields": { "summary": `)),
			},
			expectedError: "failed to decode response",
		},
		{
			name:          "API Error 500",
			args:          map[string]interface{}{"issue_key": "PROJ-1"},
			mockResp:      &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error", Body: io.NopCloser(strings.NewReader("Broken"))},
			expectedError: "jira API returned status: 500 Internal Server Error, body: Broken",
		},
		{
			name:          "Network Error",
			args:          map[string]interface{}{"issue_key": "PROJ-1"},
			mockErr:       errors.New("network down"),
			expectedError: "network down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(mockJiraClient)
			m, err := newjiraManager(nil, mockClient)
			assert.NoError(t, err)
			m.provider.baseDelay = 1 * time.Microsecond

			if tt.mockResp != nil || tt.mockErr != nil {
				mockClient.On("Do", mock.Anything).Return(tt.mockResp, tt.mockErr)
			}

			result, err := m.jiraGetIssue(context.Background(), tt.args, nil)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Contains(t, result.Text, tt.expectedText)
			}
		})
	}
}

func TestJiraManager_ParseADF(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := newjiraManager(nil, nil)
	assert.NoError(t, err)

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

	t.Run("Text followed by Paragraph", func(t *testing.T) {
		adf := []interface{}{
			map[string]interface{}{"type": "text", "text": "Plain"},
			map[string]interface{}{"type": "paragraph", "content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Para"},
			}},
		}
		result := m.parseADF(adf)
		assert.Equal(t, "Plain\nPara", result)
	})

	t.Run("Heading Newline Logic", func(t *testing.T) {
		adf := map[string]interface{}{
			"type": "doc",
			"content": []interface{}{
				map[string]interface{}{"type": "heading", "content": []interface{}{map[string]interface{}{"type": "text", "text": "H1"}}},
				map[string]interface{}{"type": "heading", "content": []interface{}{map[string]interface{}{"type": "text", "text": "H2"}}},
			},
		}
		result := m.parseADF(adf)
		assert.Equal(t, "H1\nH2", result)
	})

	t.Run("Deeply Nested ADF", func(t *testing.T) {
		adf := map[string]interface{}{
			"type": "paragraph",
			"content": []interface{}{
				map[string]interface{}{
					"type": "blockquote",
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": "Nested Text",
						},
					},
				},
			},
		}
		result := m.parseADF(adf)
		assert.Equal(t, "Nested Text", result)
	})

	t.Run("Missing Fields", func(t *testing.T) {
		adf := map[string]interface{}{
			"type": "paragraph",
			"content": []interface{}{
				map[string]interface{}{"type": "text"},     // missing "text"
				map[string]interface{}{"text": "Isolated"}, // missing "type"
				map[string]interface{}{"type": "unknown", "text": "Secret"},
			},
		}
		result := m.parseADF(adf)
		// Current implementation: if type is missing or not "text", it doesn't write.
		// If type is "text" but "text" field is missing, it doesn't write.
		assert.Equal(t, "", result)
	})

	t.Run("Invalid Content Type", func(t *testing.T) {
		adf := map[string]interface{}{
			"type":    "paragraph",
			"content": "not a slice",
		}
		result := m.parseADF(adf)
		assert.Equal(t, "", result)
	})

	t.Run("Non-string type or text", func(t *testing.T) {
		adf := map[string]interface{}{
			"type": 123,
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": 456},
			},
		}
		assert.Equal(t, "", m.parseADF(adf))
	})

	t.Run("Unsupported Node Type", func(t *testing.T) {
		assert.Equal(t, "", m.parseADF(123))
	})

	t.Run("Slice of Nodes", func(t *testing.T) {
		adf := []interface{}{
			map[string]interface{}{"type": "text", "text": "One "},
			map[string]interface{}{"type": "text", "text": "Two"},
		}
		result := m.parseADF(adf)
		assert.Equal(t, "One Two", result)
	})
}

func TestJiraManager_Constructor(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Run("Default Client", func(t *testing.T) {
		m, err := newjiraManager(nil, nil)
		assert.NoError(t, err)
		assert.NotNil(t, m.client)
	})
}

func TestJiraManager_EdgeCases(t *testing.T) {
	t.Run("Invalid Base URL Search", func(t *testing.T) {
		t.Setenv("ATLASSIAN_BASE_URL", " : invalid")
		m, err := newjiraManager(nil, nil)

		// If constructor succeeds (URL parsing happens later), check the method
		if err == nil {
			_, err = m.jiraSearchIssues(context.Background(), map[string]interface{}{"jql": "test"}, nil)
		}

		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "invalid base url") || strings.Contains(err.Error(), "failed to initialize Atlassian provider"))
	})

	t.Run("Invalid Base URL Get", func(t *testing.T) {
		t.Setenv("ATLASSIAN_BASE_URL", " : invalid")
		m, err := newjiraManager(nil, nil)

		if err == nil {
			_, err = m.jiraGetIssue(context.Background(), map[string]interface{}{"issue_key": "PROJ-1"}, nil)
		}

		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "invalid base url") || strings.Contains(err.Error(), "failed to initialize Atlassian provider"))
	})
}
