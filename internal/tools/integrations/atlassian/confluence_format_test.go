// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// xhtmlToMarkdown / markdownToXhtml tests
// ---------------------------------------------------------------------------

func TestConfluenceManager_XhtmlToMarkdown(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := NewConfluenceManager(nil, nil)
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

func TestConfluenceManager_MarkdownToXhtml(t *testing.T) {
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	m, err := NewConfluenceManager(nil, nil)
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
