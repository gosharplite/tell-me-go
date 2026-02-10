// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockConfluenceClient struct {
	mock.Mock
}

func (m *mockConfluenceClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestConfluenceManager_GetAuthHeader(t *testing.T) {
	m := NewConfluenceManager(nil, nil)

	t.Run("Missing Email", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "")
		t.Setenv("ATLASSIAN_TOKEN", "token")
		_, err := m.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Missing ATLASSIAN_EMAIL")
	})

	t.Run("Missing Token", func(t *testing.T) {
		t.Setenv("ATLASSIAN_EMAIL", "email")
		t.Setenv("ATLASSIAN_TOKEN", "")
		_, err := m.getAuthHeader()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Missing ATLASSIAN_TOKEN")
	})

	t.Run("Success", func(t *testing.T) {
		email := "test@example.com"
		token := "api-token"
		t.Setenv("ATLASSIAN_EMAIL", email)
		t.Setenv("ATLASSIAN_TOKEN", token)
		header, err := m.getAuthHeader()
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
		m := NewConfluenceManager(nil, mockClient)

		jsonResponse := `{
			"results": [
				{"id": "1", "title": "Page 1"},
				{"id": "2", "title": "Page 2"}
			]
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return strings.HasPrefix(req.URL.String(), "https://test.atlassian.net/wiki/api/v2/pages") &&
				req.URL.Query().Get("title") == "Test" &&
				req.URL.Query().Get("space-id") == "SPACE1"
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{
			"title":    "Test",
			"space_id": "SPACE1",
		}

		result, err := m.confluenceSearch(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Found pages:")
		assert.Contains(t, result.Text, "Page 1 (ID: 1)")
		assert.Contains(t, result.Text, "Page 2 (ID: 2)")
	})

	t.Run("Success Space Only", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		jsonResponse := `{
			"results": [
				{"id": "3", "title": "Space Page"}
			]
		}`

		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.URL.Query().Get("space-id") == "SPACE1" && req.URL.Query().Get("title") == ""
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{
			"space_id": "SPACE1",
		}

		result, err := m.confluenceSearch(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Space Page (ID: 3)")
	})

	t.Run("No Results with Hint", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		jsonResponse := `{"results": []}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"title": "missing"}
		result, err := m.confluenceSearch(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "No pages found")
		assert.Contains(t, result.Text, "Hint: The V2 API requires an exact title match")
	})

	t.Run("API Error", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{"title": "error"}
		_, err := m.confluenceSearch(context.Background(), args)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "Confluence API returned status: 403 Forbidden")
		}
	})
}

func TestConfluenceManager_ConfluenceRead(t *testing.T) {
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")

	t.Run("Success", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

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

		result, err := m.confluenceRead(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "# Test Page")
		assert.Contains(t, result.Text, "# Title")
		assert.Contains(t, result.Text, "Hello World")
		assert.Contains(t, result.Text, "* Item 1")
		assert.Contains(t, result.Text, "* Item 2")
	})

	t.Run("Page Not Found", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"page_id": "404",
		}

		_, err := m.confluenceRead(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Confluence page not found: 404")
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusUnauthorized,
			Status:     "401 Unauthorized",
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil)

		args := map[string]interface{}{
			"page_id": "123",
		}

		_, err := m.confluenceRead(context.Background(), args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed: 401 Unauthorized")
	})
}

func TestConfluenceManager_XhtmlToMarkdown(t *testing.T) {
	m := NewConfluenceManager(nil, nil)

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
		sm := security.NewSecurityManager(strings.NewReader(input))
		m := NewConfluenceManager(sm, mockClient)

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

		result, err := m.confluenceWrite(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Successfully updated Confluence page 123 to version 6")
		mockClient.AssertExpectations(t)
	})

	t.Run("Cancelled by user", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		input := "n\n"
		sm := security.NewSecurityManager(strings.NewReader(input))
		m := NewConfluenceManager(sm, mockClient)

		// Mock GET version
		jsonVersion := `{"id": "123", "title": "Old Title", "version": {"number": 5}}`
		mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
			return req.Method == http.MethodGet
		})).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonVersion)),
		}, nil)

		args := map[string]interface{}{
			"page_id":          "123",
			"markdown_content": "some content",
		}

		result, err := m.confluenceWrite(context.Background(), args)
		assert.NoError(t, err)
		assert.Equal(t, "Action cancelled by user.", result.Text)
	})

	t.Run("Conflict 409", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		input := "y\n"
		sm := security.NewSecurityManager(strings.NewReader(input))
		m := NewConfluenceManager(sm, mockClient)

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

		result, err := m.confluenceWrite(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "Conflict: The page version has changed")
	})
}

func TestConfluenceManager_MarkdownToXhtml(t *testing.T) {
	m := NewConfluenceManager(nil, nil)

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
	mockClient := new(mockConfluenceClient)
	m := NewConfluenceManager(nil, mockClient)

	largeBody := strings.Repeat("A", 5*1024*1024+10)
	mockClient.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(largeBody)),
	}, nil)

	args := map[string]interface{}{
		"page_id": "123",
	}

	_, err := m.confluenceRead(context.Background(), args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Confluence page size exceeds the 5MB limit")
}

func TestConfluenceManager_ConfluenceSearch_EmptyResults(t *testing.T) {
	t.Run("Null Results", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		jsonResponse := `{"results": null}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"title": "nothing"}
		result, err := m.confluenceSearch(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "No pages found matching the criteria.")
	})

	t.Run("Missing Results", func(t *testing.T) {
		mockClient := new(mockConfluenceClient)
		m := NewConfluenceManager(nil, mockClient)

		jsonResponse := `{}`

		mockClient.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}, nil)

		args := map[string]interface{}{"title": "nothing"}
		result, err := m.confluenceSearch(context.Background(), args)
		assert.NoError(t, err)
		assert.Contains(t, result.Text, "No pages found matching the criteria.")
	})
}
