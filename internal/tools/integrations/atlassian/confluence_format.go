// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"html"
	"regexp"
	"strings"
)

var (
	reH1            = regexp.MustCompile(`(?i)<h1.*?>`)
	reH2            = regexp.MustCompile(`(?i)<h2.*?>`)
	reH3            = regexp.MustCompile(`(?i)<h3.*?>`)
	reH4            = regexp.MustCompile(`(?i)<h4.*?>`)
	reH5            = regexp.MustCompile(`(?i)<h5.*?>`)
	reH6            = regexp.MustCompile(`(?i)<h6.*?>`)
	reCloseHeader   = regexp.MustCompile(`(?i)</h[1-6]>`)
	reLi            = regexp.MustCompile(`(?i)<li.*?>`)
	reCloseLi       = regexp.MustCompile(`(?i)</li>`)
	reBr            = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlocks        = regexp.MustCompile(`(?i)</?(p|div|tr|td|table|ul|ol).*?>`)
	reTags          = regexp.MustCompile(`<.*?>`)
	reMultiSpace    = regexp.MustCompile(` +`)
	reLeadingSpace  = regexp.MustCompile(`(?m)^ +`)
	reTrailingSpace = regexp.MustCompile(`(?m) +$`)
	reMultiNewline  = regexp.MustCompile(`\n\n+`)

	reBold1   = regexp.MustCompile(`\*\*(.*?)\*\*`)
	reBold2   = regexp.MustCompile(`__(.*?)__`)
	reItalic1 = regexp.MustCompile(`\*(.*?)\*`)
	reItalic2 = regexp.MustCompile(`_(.*?)_`)
)

// headingTags maps Markdown heading prefixes to their XHTML tag names.
var headingTags = map[string]string{
	"# ":      "h1",
	"## ":     "h2",
	"### ":    "h3",
	"#### ":   "h4",
	"##### ":  "h5",
	"###### ": "h6",
}

// convertHeading detects a Markdown ATX heading line and returns the XHTML tag,
// escaped content, and true. Returns zero values and false for non-heading lines.
func convertHeading(line string) (tag string, content string, ok bool) {
	for prefix, tag := range headingTags {
		if strings.HasPrefix(line, prefix) {
			return tag, html.EscapeString(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", "", false
}

func (m *ConfluenceManager) xhtmlToMarkdown(xhtml string) string {
	// 1. Normalize whitespace - replace all newlines and tabs with spaces
	content := strings.ReplaceAll(xhtml, "\r", "")
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\t", " ")

	// 2. Handle headers
	content = reH1.ReplaceAllString(content, "\n\n# ")
	content = reH2.ReplaceAllString(content, "\n\n## ")
	content = reH3.ReplaceAllString(content, "\n\n### ")
	content = reH4.ReplaceAllString(content, "\n\n#### ")
	content = reH5.ReplaceAllString(content, "\n\n##### ")
	content = reH6.ReplaceAllString(content, "\n\n###### ")
	content = reCloseHeader.ReplaceAllString(content, "\n\n")

	// 3. Handle lists
	content = reLi.ReplaceAllString(content, "\n* ")
	content = reCloseLi.ReplaceAllString(content, "")

	// 4. Handle line breaks and block elements
	content = reBr.ReplaceAllString(content, "\n")
	content = reBlocks.ReplaceAllString(content, "\n\n")

	// 5. Strip all remaining HTML tags
	content = reTags.ReplaceAllString(content, "")

	// 6. Unescape HTML entities
	content = html.UnescapeString(content)
	// Replace &nbsp; (\u00a0) with regular space for test compatibility
	content = strings.ReplaceAll(content, "\u00a0", " ")

	// 7. Clean up whitespace
	// Multi-space to single space
	content = reMultiSpace.ReplaceAllString(content, " ")

	// Remove leading/trailing spaces on each line
	content = reLeadingSpace.ReplaceAllString(content, "")
	content = reTrailingSpace.ReplaceAllString(content, "")

	// Fix multiple newlines
	content = reMultiNewline.ReplaceAllString(content, "\n\n")

	return strings.TrimSpace(content)
}

func (m *ConfluenceManager) markdownToXhtml(markdown string) string {
	lines := strings.Split(markdown, "\n")
	var result strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if tag, content, ok := convertHeading(line); ok {
			result.WriteString("<" + tag + ">" + content + "</" + tag + ">")
		} else {
			// Escape first to avoid escaping generated tags
			line = html.EscapeString(line)
			// Apply inline formatting
			line = reBold1.ReplaceAllString(line, "<b>$1</b>")
			line = reBold2.ReplaceAllString(line, "<b>$1</b>")
			line = reItalic1.ReplaceAllString(line, "<i>$1</i>")
			line = reItalic2.ReplaceAllString(line, "<i>$1</i>")
			result.WriteString("<p>" + line + "</p>")
		}
	}
	return result.String()
}
