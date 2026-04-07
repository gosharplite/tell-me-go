// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockConfluenceClient struct {
	mock.Mock
}

func (m *mockConfluenceClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestConfluenceManager_GetAuthHeader(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Run("Missing Email", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "")
		t.Setenv("ATLASSIAN_TOKEN", "token")
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.provider.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_EMAIL")
	})

	t.Run("Missing Token", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "email")
		t.Setenv("ATLASSIAN_TOKEN", "")
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.provider.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_TOKEN")
	})

	t.Run("Success", func(t *testing.T) {
		email := "test@example.com"
		token := "api-token"
		t.Setenv("ATLASSIAN_EMAIL", email)
		t.Setenv("ATLASSIAN_TOKEN", token)
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		header, err := m.provider.getAuthHeader()
		assert.NoError(t, err)

		expectedAuth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
		assert.Equal(t, "Basic "+expectedAuth, header)
	})
}

func TestConfluenceManager_ConfluenceSearch(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("Success with Title and Space", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		// 1. Mock Space Resolution
		jsonSpace := `{"results": [{"id": "123"}]}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/wiki/api/v2/spaces?keys=SPACE1")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonSpace)),
		}, nil).Once()

		// 2. Mock Pages Search
		jsonResponse := `{
			"results": [
				{"id": "1", "title": "Test Page 1"},
				{"id": "2", "title": "Other Page"}
			]
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.HasPrefix(req.URL.String(), "https://test.atlassian.net/wiki/api/v2/pages") &&
				req.URL.Query().Get("space-id") == "123" &&
				req.URL.Query().Get("limit") == "50"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

		args := map[string]interface{}{
			"title":    "Test",
			"space_id": "SPACE1",
		}

		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Found pages:")
		assert.Contains(t, result.Text, "Test Page 1 (ID: 1)")
		assert.NotContains(t, result.Text, "Other Page")
		mockClient.AssertExpectations(t)
	})

	t.Run("Success Space Only", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		// 1. Mock Space Resolution
		jsonSpace := `{"results": [{"id": "123"}]}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/wiki/api/v2/spaces?keys=SPACE1")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonSpace)),
		}, nil).Once()

		// 2. Mock Pages Search
		jsonResponse := `{
			"results": [
				{"id": "3", "title": "Space Page"}
			]
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.URL.Query().Get("space-id") == "123"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil).Once()

		args := map[string]interface{}{
			"space_id": "SPACE1",
		}

		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Space Page (ID: 3)")
		mockClient.AssertExpectations(t)
	})

	t.Run("No Results with Hint", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		// 1. Mock Space Resolution (using numeric ID this time to skip resolution)
		jsonResponse := `{"results": [{"id":"1", "title":"Random"}], "_links": {"next": ""}}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{
			"title":    "missing",
			"space_id": "123", // Numeric, skips resolution
		}
		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Searched the 1 most recently modified pages")
		assert.Contains(t, result.Text, "found no pages containing 'missing'")
	})

	t.Run("Success Incremental Discovery", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		// Numeric space_id to skip resolution for brevity in this test
		spaceID := "123"

		// Page 1: No matches, has next link
		jsonPage1 := `{
			"results": [
				{"id": "1", "title": "Random Page"},
				{"id": "2", "title": "Other Doc"}
			],
			"_links": {
				"next": "/wiki/api/v2/pages?cursor=next-cursor"
			}
		}`

		// Page 2: Contains match
		jsonPage2 := `{
			"results": [
				{"id": "3", "title": "[cicd] Azure DevOps"}
			],
			"_links": {
				"next": ""
			}
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return !strings.Contains(req.URL.String(), "cursor=next-cursor")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonPage1)),
		}, nil).Once()

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "cursor=next-cursor")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonPage2)),
		}, nil).Once()

		args := map[string]interface{}{
			"title":    "cicd",
			"space_id": spaceID,
		}

		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "[cicd] Azure DevOps (ID: 3)")
		mockClient.AssertExpectations(t)
	})

	t.Run("Error Title without Space", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		args := map[string]interface{}{"title": "keyword"}
		_, err = m.confluenceSearch(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "space_id is required")
	})

	t.Run("API Error", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"title":    "error",
			"space_id": "123",
		}
		_, err = m.confluenceSearch(context.Background(), args, nil)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "confluence API returned status: 403 Forbidden")
		}
	})

	t.Run("Success with Limit 100", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		// Page 1: No matches, has next link
		jsonPage1 := `{"results": [{"id": "1", "title": "Random"}], "_links": {"next": "/next"}}`
		// Page 2: No matches
		jsonPage2 := `{"results": [{"id": "2", "title": "Random"}], "_links": {"next": ""}}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonPage1)),
		}, nil).Once()

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonPage2)),
		}, nil).Once()

		args := map[string]interface{}{
			"title":    "cicd",
			"space_id": "123",
			"limit":    100,
		}

		_, err = m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		mockClient.AssertNumberOfCalls(t, "Do", 2)
	})

	t.Run("Capped at 1000", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		jsonResponse := `{"results": [], "_links": {"next": "/next"}}`
		for i := 0; i < 20; i++ {
			mockClient.On("Do", mock.Anything).Return(&http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(jsonResponse)),
			}, nil).Once()
		}

		args := map[string]interface{}{
			"title":    "cicd",
			"space_id": "123",
			"limit":    5000,
		}

		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "capped at 1000 pages")
		mockClient.AssertNumberOfCalls(t, "Do", 20)
	})
}

func TestConfluenceManager_ConfluenceRead(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		jsonResponse := `{
			"title": "Test Page",
			"body": {
				"storage": {
					"value": "<h1>Title</h1><p>Hello <b>World</b></p><ul><li>Item 1</li><li>Item 2</li></ul>"
				}
			}
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.Contains(req.URL.String(), "/pages/123") && req.URL.Query().Get("body-format") == "storage" && strings.HasPrefix(req.URL.String(), "https://test.atlassian.net")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{
			"page_id": "123",
		}

		result, err := m.confluenceRead(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "# Test Page")
		assert.Contains(t, result.Text, "# Title")
		assert.Contains(t, result.Text, "Hello World")
		assert.Contains(t, result.Text, "* Item 1")
		assert.Contains(t, result.Text, "* Item 2")
	})

	t.Run("Page Not Found", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"page_id": "404",
		}

		_, err = m.confluenceRead(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "confluence page not found: 404")
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusUnauthorized,
			Status:     "401 Unauthorized",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"page_id": "123",
		}

		_, err = m.confluenceRead(context.Background(), args, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed: 401 Unauthorized")
	})
}

func TestConfluenceManager_XhtmlToMarkdown(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := newconfluenceManager(nil, nil)
	assert.NoError(t, err)

	tests := []struct {
		name     string
		xhtml    string
		expected string
	}{
		{
			name:     "Basic content",
			xhtml:    "<h1>Title</h1><p>Hello <b>World</b></p>",
			expected: "# Title\n\nHello World",
		},
		{
			name:     "Nested Elements",
			xhtml:    "<div><p>Text with <b>bold</b> and <i>italic</i></p></div>",
			expected: "Text with bold and italic",
		},
		{
			name:     "Attributes",
			xhtml:    "<h1 id=\"main\" class=\"title\">Header</h1><p style=\"color:red\">Paragraph</p>",
			expected: "# Header\n\nParagraph",
		},
		{
			name:     "Extra Whitespace",
			xhtml:    "<h1>  Title  </h1><p>\tHello \n World\t</p>",
			expected: "# Title\n\nHello World",
		},
		{
			name:     "Entities",
			xhtml:    "<p>Entities: &nbsp; &lt; &gt; &amp; &quot;</p>",
			expected: "Entities: < > & \"",
		},
		{
			name:     "Lists and Spacing",
			xhtml:    "<ul><li>Item 1</li><li>Item 2</li></ul><div>Block</div>",
			expected: "* Item 1\n* Item 2\n\nBlock",
		},
		{
			name: "Complex Spacing",
			xhtml: `
				<h1>Heading 1</h1>
				<p>Paragraph with <br/> a break and <b>bold</b> text.</p>
				<h2>Heading 2</h2>
				<ul>
					<li>List Item 1</li>
					<li>List Item 2 &amp; more</li>
				</ul>
				<div>Div content</div>
			`,
			expected: "# Heading 1\n\nParagraph with\na break and bold text.\n\n## Heading 2\n\n* List Item 1\n* List Item 2 & more\n\nDiv content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.xhtmlToMarkdown(tt.xhtml)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfluenceManager_ConfluenceWrite(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		input := "y\n"
		sm := security.NewSecurityManager(&security.MockInteractor{Answer: input})
		m, err := newconfluenceManager(sm, mockClient)
		assert.NoError(t, err)

		pageID := "123"

		// 1. Mock GET version
		jsonVersion := `{"id": "123", "title": "Old Title", "version": {"number": 5}}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.Method == http.MethodGet && strings.Contains(req.URL.String(), "/pages/123") && strings.HasPrefix(req.URL.String(), "https://test.atlassian.net")
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonVersion)),
		}, nil)

		// 2. Mock PUT update
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			if req.Method != http.MethodPut {
				return false
			}
			if !strings.HasPrefix(req.URL.String(), "https://test.atlassian.net") {
				return false
			}
			var payload map[string]interface{}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return false
			}
			version, ok := payload["version"].(map[string]interface{})
			if !ok {
				return false
			}
			return version["number"] == float64(6) && payload["title"] == "New Title"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "123"}`)),
		}, nil)

		args := map[string]interface{}{
			"page_id":          pageID,
			"title":            "New Title",
			"markdown_content": "# New Content",
			"update_message":   "testing update",
		}

		result, err := m.confluenceWrite(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Successfully updated Confluence page 123 to version 6")
		mockClient.AssertExpectations(t)
	})

	t.Run("Conflict 409", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		input := "y\n"
		sm := security.NewSecurityManager(&security.MockInteractor{Answer: input})
		m, err := newconfluenceManager(sm, mockClient)
		assert.NoError(t, err)

		// Mock GET version
		jsonVersion := `{"id": "123", "title": "Old Title", "version": {"number": 5}}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.Method == http.MethodGet
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonVersion)),
		}, nil)

		// Mock PUT conflict
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.Method == http.MethodPut
		})).Return(&http.Response{
			StatusCode: http.StatusConflict,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"page_id":          "123",
			"markdown_content": "some content",
		}

		result, err := m.confluenceWrite(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "conflict: page version changed")
	})
}

func TestConfluenceManager_MarkdownToXhtml(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := newconfluenceManager(nil, nil)
	assert.NoError(t, err)

	tests := []struct {
		name     string
		markdown string
		expected string
	}{
		{
			name:     "Basic content",
			markdown: "# Header 1\n\nSome text here.",
			expected: "<h1>Header 1</h1><p>Some text here.</p>",
		},
		{
			name:     "Inline formatting",
			markdown: "Text with **bold**, __bold__, *italic*, and _italic_.",
			expected: "<p>Text with <b>bold</b>, <b>bold</b>, <i>italic</i>, and <i>italic</i>.</p>",
		},
		{
			name:     "Escaping with inline",
			markdown: "Text with <script>alert(1)</script> and **bold**.",
			expected: "<p>Text with &lt;script&gt;alert(1)&lt;/script&gt; and <b>bold</b>.</p>",
		},
		{
			name:     "Headers and paragraphs",
			markdown: "# H1\n## H2\nPara",
			expected: "<h1>H1</h1><h2>H2</h2><p>Para</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.markdownToXhtml(tt.markdown)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfluenceManager_ConfluenceRead_LargePayload(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "mock-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	mockClient := new(mockConfluenceClient)
	m, err := newconfluenceManager(nil, mockClient)
	assert.NoError(t, err)

	largeBody := strings.Repeat("A", 5*1024*1024+10)
	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(largeBody)),
	}, nil)

	args := map[string]interface{}{
		"page_id": "123",
	}

	_, err = m.confluenceRead(context.Background(), args, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "confluence page size exceeds the 5MB limit")
}

func TestConfluenceManager_ConfluenceSearch_EmptyResults(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "mock-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Run("Null Results", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		jsonResponse := `{"results": null}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{
			"title":    "nothing",
			"space_id": "123",
		}
		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "found no pages containing 'nothing'")
	})

	t.Run("Missing Results", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		jsonResponse := `{}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{
			"title":    "nothing",
			"space_id": "123",
		}
		result, err := m.confluenceSearch(context.Background(), args, nil)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "found no pages containing 'nothing'")
	})
}

func TestConfluenceManager_ResolveSpaceID(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	tests := []struct {
		name          string
		spaceKey      string
		mockResp      string
		mockStatus    int
		mockErr       error
		baseURL       string
		expectedID    string
		expectedError string
	}{
		{
			name:       "Numeric ID",
			spaceKey:   "12345",
			expectedID: "12345",
		},
		{
			name:       "Resolution Success",
			spaceKey:   "PROJ",
			mockResp:   `{"results": [{"id": "98765"}]}`,
			mockStatus: http.StatusOK,
			expectedID: "98765",
		},
		{
			name:          "Resolution Not Found (Empty Results)",
			spaceKey:      "MISSING",
			mockResp:      `{"results": []}`,
			mockStatus:    http.StatusOK,
			expectedError: "space key 'MISSING' not found",
		},
		{
			name:          "Resolution Not Found (404)",
			spaceKey:      "NOTFOUND",
			mockStatus:    http.StatusNotFound,
			expectedError: "space key 'NOTFOUND' not found",
		},
		{
			name:          "API Failure (500)",
			spaceKey:      "ERROR",
			mockStatus:    http.StatusInternalServerError,
			expectedError: "failed to resolve space key 'ERROR', status: 500",
		},
		{
			name:          "Network Error",
			spaceKey:      "NETWORK",
			mockErr:       fmt.Errorf("network error"),
			expectedError: "network error",
		},
		{
			name:          "Auth Error",
			spaceKey:      "AUTH_ERR",
			mockStatus:    http.StatusOK, // won't be used
			expectedError: "missing ATLASSIAN_EMAIL",
		},
		{
			name:          "Decode Error",
			spaceKey:      "DECODE_ERR",
			mockResp:      `{invalid json}`,
			mockStatus:    http.StatusOK,
			expectedError: "invalid character",
		},
		{
			name:          "Invalid URL",
			spaceKey:      "INVALID_URL",
			baseURL:       " ://invalid",
			expectedError: "parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Auth Error" {
				t.Setenv("ATLASSIAN_EMAIL", "")
			} else {
				t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
			}
			if tt.baseURL != "" {
				t.Setenv("ATLASSIAN_BASE_URL", tt.baseURL)
			} else {
				t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
			}
			mockClient := new(mockConfluenceClient)
			m, err := newconfluenceManager(nil, mockClient)
			// Handle constructor error which might happen if baseURL is invalid
			if err != nil {
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
					return
				}
				t.Fatalf("unexpected constructor error: %v", err)
			}

			if tt.spaceKey == "12345" || tt.name == "Auth Error" || tt.name == "Invalid URL" {
				// No mock needed
			} else {
				if tt.mockErr != nil {
					mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), tt.mockErr)
				} else {
					resp := &http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(strings.NewReader(tt.mockResp)),
						Status:     fmt.Sprintf("%d", tt.mockStatus),
					}
					mockClient.On("Do", mock.Anything).Return(resp, nil)
				}
			}

			id, err := m.resolveSpaceID(context.Background(), tt.spaceKey)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
			if tt.spaceKey != "12345" {
				mockClient.AssertExpectations(t)
			}
		})
	}
}

func TestConfluenceManager_ConfluenceSearch_ResolveError(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	mockClient := new(mockConfluenceClient)
	m, err := newconfluenceManager(nil, mockClient)
	assert.NoError(t, err)

	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil)

	args := map[string]interface{}{
		"title":    "test",
		"space_id": "BADSPACE",
	}

	_, err = m.confluenceSearch(context.Background(), args, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve space ID for 'BADSPACE'")
}

func TestConfluenceManager_ResolveNextURL(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := newconfluenceManager(nil, nil)
	assert.NoError(t, err)
	baseURL := "https://test.atlassian.net"

	tests := []struct {
		name     string
		nextPath string
		expected string
	}{
		{
			name:     "Empty path",
			nextPath: "",
			expected: "",
		},
		{
			name:     "Absolute URL",
			nextPath: "https://example.com/next",
			expected: "https://example.com/next",
		},
		{
			name:     "Relative path",
			nextPath: "/wiki/api/v2/pages?cursor=123",
			expected: "https://test.atlassian.net/wiki/api/v2/pages?cursor=123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.resolveNextURL(baseURL, tt.nextPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfluenceManager_ProcessSearchResults(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := newconfluenceManager(nil, nil)
	assert.NoError(t, err)
	results := []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{
		{ID: "1", Title: "Design Doc"},
		{ID: "2", Title: "Implementation Plan"},
		{ID: "3", Title: "Testing Strategy"},
	}

	tests := []struct {
		name     string
		keyword  string
		expected []searchMatch
	}{
		{
			name:    "Empty keyword",
			keyword: "",
			expected: []searchMatch{
				{ID: "1", Title: "Design Doc"},
				{ID: "2", Title: "Implementation Plan"},
				{ID: "3", Title: "Testing Strategy"},
			},
		},
		{
			name:    "Exact match case insensitive",
			keyword: "DESIGN",
			expected: []searchMatch{
				{ID: "1", Title: "Design Doc"},
			},
		},
		{
			name:    "Partial match",
			keyword: "plan",
			expected: []searchMatch{
				{ID: "2", Title: "Implementation Plan"},
			},
		},
		{
			name:     "No match",
			keyword:  "meeting",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.processSearchResults(results, tt.keyword)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfluenceManager_ExhaustiveErrors(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("UnmarshalArgs Error Search", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.confluenceSearch(context.Background(), map[string]interface{}{"limit": "invalid"}, nil)
		assert.Error(t, err)
	})

	t.Run("UnmarshalArgs Error Read", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.confluenceRead(context.Background(), map[string]interface{}{"page_id": 123}, nil) // Should be string
		assert.Error(t, err)
	})

	t.Run("UnmarshalArgs Error Write", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.confluenceWrite(context.Background(), map[string]interface{}{"page_id": 123}, nil)
		assert.Error(t, err)
	})

	t.Run("Malformed Base URL Search", func(t *testing.T) {
		t.Setenv("ATLASSIAN_BASE_URL", "http://bad\x7furl")
		m, err := newconfluenceManager(nil, nil)
		if err == nil {
			_, err = m.confluenceSearch(context.Background(), map[string]interface{}{"space_id": "123"}, nil)
		}
		require.Error(t, err)
		// Error could be from constructor or method
		assert.True(t, strings.Contains(err.Error(), "invalid base url") || strings.Contains(err.Error(), "failed to initialize Atlassian provider"))
	})

	t.Run("Network Failure Read", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), fmt.Errorf("network down"))

		_, err = m.confluenceRead(context.Background(), map[string]interface{}{"page_id": "123"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed: network down")
	})

	t.Run("HTTP 500 Read", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 500,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(strings.NewReader("error")),
		}, nil)

		_, err = m.confluenceRead(context.Background(), map[string]interface{}{"page_id": "123"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "confluence API returned status: 500")
	})

	t.Run("Invalid JSON Read", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{ invalid ")),
		}, nil)

		_, err = m.confluenceRead(context.Background(), map[string]interface{}{"page_id": "123"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response")
	})

	t.Run("Auth Failure Write", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "")
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.confluenceWrite(context.Background(), map[string]interface{}{"page_id": "123", "markdown_content": "test"}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_EMAIL")
	})

	t.Run("Auth Failure resolveSpaceID", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "")
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.resolveSpaceID(context.Background(), "SPACE")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing ATLASSIAN_EMAIL")
	})

	t.Run("fetchSearchPage Error request creation", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.fetchSearchPage(context.Background(), " ://bad")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create request")
	})

	t.Run("fetchSearchPage Error Do", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), fmt.Errorf("do error"))
		_, err = m.fetchSearchPage(context.Background(), "https://test.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "request failed")
	})

	t.Run("getCurrentPageVersion Error request creation", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		t.Setenv("ATLASSIAN_BASE_URL", " ://bad")
		// Need to manually overwrite baseURL because manager was already created
		m.provider.baseURL = " ://bad"
		_, err = m.getCurrentPageVersion(context.Background(), "123")
		assert.Error(t, err)
	})

	t.Run("executeUpdate Error request creation", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		t.Setenv("ATLASSIAN_BASE_URL", " ://bad")
		m.provider.baseURL = " ://bad"
		err = m.executeUpdate(context.Background(), "123", nil)
		assert.Error(t, err)
	})

	t.Run("truncate test", func(t *testing.T) {
		assert.Equal(t, "abc", truncate("abc", 5))
		assert.Equal(t, "abc...", truncate("abcd", 3))
	})

	t.Run("MarkdownToXhtml H3-H6", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		assert.Contains(t, m.markdownToXhtml("### H3"), "<h3>H3</h3>")
		assert.Contains(t, m.markdownToXhtml("#### H4"), "<h4>H4</h4>")
		assert.Contains(t, m.markdownToXhtml("##### H5"), "<h5>H5</h5>")
		assert.Contains(t, m.markdownToXhtml("###### H6"), "<h6>H6</h6>")
	})
}

func TestConfluenceManager_ExhaustiveErrors_V2(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("resolveNextURL_MalformedBase", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		res := m.resolveNextURL(" ://bad", "path")
		assert.Equal(t, "", res)
	})

	t.Run("resolveNextURL_MalformedPath", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		res := m.resolveNextURL("https://test.com", " ://bad")
		assert.Equal(t, "", res)
	})

	t.Run("formatSearchResults_EmptyTitle", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		res := m.formatSearchResults(nil, "warn", 10, "space", "")
		assert.Contains(t, res.Text, "No pages found")
	})

	t.Run("getCurrentPageVersion_DoError", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), fmt.Errorf("error"))
		_, err = m.getCurrentPageVersion(context.Background(), "123")
		assert.Error(t, err)
	})

	t.Run("getCurrentPageVersion_DecodeError", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{ invalid ")),
		}, nil)
		_, err = m.getCurrentPageVersion(context.Background(), "123")
		assert.Error(t, err)
	})

	t.Run("executeUpdate_DoError", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return((*http.Response)(nil), fmt.Errorf("error"))
		err = m.executeUpdate(context.Background(), "123", nil)
		assert.Error(t, err)
	})

	t.Run("executeUpdate_MarshalError", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		// Map with non-marshalable key (complex128)
		payload := map[string]interface{}{
			"bad": make(chan int),
		}
		err = m.executeUpdate(context.Background(), "123", payload)
		assert.Error(t, err)
	})
}

func TestConfluenceManager_ExhaustiveErrors_V3(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("resolveSpaceIDIfNeeded_Empty", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		res, err := m.resolveSpaceIDIfNeeded(context.Background(), "")
		assert.NoError(t, err)
		assert.Equal(t, "", res)
	})

	t.Run("fetchPageContent_ReqError", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		_, err = m.fetchPageContent(context.Background(), " ://bad")
		assert.Error(t, err)
	})

	t.Run("fetchPageContent_Forbidden", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)
		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: 403,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)
		_, err = m.fetchPageContent(context.Background(), "123")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})

	t.Run("readAndValidateBody_ReadError", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		// Custom reader that returns error
		errReader := &errorReader{err: fmt.Errorf("read error")}
		_, err = m.readAndValidateBody(io.NopCloser(errReader))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read")
	})
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

func TestConfluenceManager_SecureURLConstruction(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	specialID := "123?456&789#abc"
	specialKey := "SPACE&KEY#ETC"

	t.Run("resolveSpaceID with special characters", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			// Check if the query parameter is correctly escaped
			q := req.URL.Query()
			return q.Get("keys") == specialKey
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"results": [{"id": "98765"}]}`)),
		}, nil)

		id, err := m.resolveSpaceID(context.Background(), specialKey)
		assert.NoError(t, err)
		assert.Equal(t, "98765", id)
		mockClient.AssertExpectations(t)
	})

	t.Run("fetchPageContent with special characters", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			// Check if the path variable is correctly escaped
			expectedPath := "/wiki/api/v2/pages/" + url.PathEscape(specialID)
			return strings.Contains(req.URL.EscapedPath(), expectedPath) && req.URL.Query().Get("body-format") == "storage"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"title": "Test", "body": {"storage": {"value": "content"}}}`)),
		}, nil)

		resp, err := m.fetchPageContent(context.Background(), specialID)
		assert.NoError(t, err)
		_ = resp.Body.Close()
		mockClient.AssertExpectations(t)
	})

	t.Run("getCurrentPageVersion with special characters", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			expectedPath := "/wiki/api/v2/pages/" + url.PathEscape(specialID)
			return strings.Contains(req.URL.EscapedPath(), expectedPath)
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "123", "title": "Test", "version": {"number": 1}}`)),
		}, nil)

		_, err = m.getCurrentPageVersion(context.Background(), specialID)
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("executeUpdate with special characters", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m, err := newconfluenceManager(nil, mockClient)
		assert.NoError(t, err)

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			expectedPath := "/wiki/api/v2/pages/" + url.PathEscape(specialID)
			return req.Method == http.MethodPut && strings.Contains(req.URL.EscapedPath(), expectedPath)
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id": "123"}`)),
		}, nil)

		err = m.executeUpdate(context.Background(), specialID, map[string]interface{}{"title": "New"})
		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("prepareSearchURL with special characters", func(t *testing.T) {
		m, err := newconfluenceManager(nil, nil)
		assert.NoError(t, err)
		u, err := m.prepareSearchURL(specialID)
		assert.NoError(t, err)
		assert.Equal(t, specialID, u.Query().Get("space-id"))
		assert.Contains(t, u.String(), "space-id=123%3F456%26789")
	})
}
