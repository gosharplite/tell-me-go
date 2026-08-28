// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// maxResponsesImageEdgePx is the client-side image dimension cap for the
// /responses image path (spec §3, D3). Drift-verified 2026-09 against the
// OpenAI images-vision guide: 2048 px longest edge (provider-side rejection
// threshold 6000 px; enforcement dimension is the longest edge).
const maxResponsesImageEdgePx = 2048

// imageLongestEdge decodes an image header and returns max(width, height).
// Uses stdlib image.DecodeConfig (header-only, no full decode). An error is
// returned for undecodable formats (e.g. WebP) — callers skip those parts
// (the provider enforces them) rather than failing the turn.
func imageLongestEdge(data []byte) (int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	m := cfg.Width
	if cfg.Height > m {
		m = cfg.Height
	}
	return m, nil
}

// checkResponsesImageDimensions enforces the 2048 px longest-edge cap for
// the /responses image path (spec §3, D3). Fails loud via
// llm.NewMediaSizeError BEFORE any request; undecodable formats are skipped.
// Video parts are ignored (vision-routed models drop them upstream).
func (c *client) checkResponsesImageDimensions(parts []*llm.Part) error {
	for _, p := range parts {
		if p.InlineData == nil || len(p.InlineData.Data) == 0 || !strings.HasPrefix(p.InlineData.MIMEType, "image/") {
			continue
		}
		edge, err := imageLongestEdge(p.InlineData.Data)
		if err != nil {
			continue // undecodable format (e.g. WebP) → provider-enforced, documented trade-off
		}
		if edge > maxResponsesImageEdgePx {
			return llm.NewMediaSizeError(llm.MediaSizePerImage, llm.MediaSizeModeLongestEdge, maxResponsesImageEdgePx, edge)
		}
	}
	return nil
}
