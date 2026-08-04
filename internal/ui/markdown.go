// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"github.com/charmbracelet/glamour"
)

// NewMarkdownRenderer builds a glamour renderer with the project defaults
// (GLAMOUR_STYLE-driven style + emoji), appending any caller options.
// Caller options are appended last so they can override or augment the
// project defaults (e.g. a test-injected failing option, or a custom
// word-wrap width).
func NewMarkdownRenderer(opts ...glamour.TermRendererOption) (markdownRenderer, error) {
	return glamour.NewTermRenderer(append([]glamour.TermRendererOption{
		glamour.WithStandardStyle(resolveGlamourStyle()),
		glamour.WithEmoji(),
	}, opts...)...)
}
