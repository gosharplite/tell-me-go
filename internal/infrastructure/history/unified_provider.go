// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// unifiedProvider implements ports.UnifiedHistoryProvider by stitching
// active memory history and archived disk history.
type unifiedProvider struct {
	archive ports.ArchiveReader
	active  ports.HistoryManager
}

// NewUnifiedProvider creates a new UnifiedProvider.
func NewUnifiedProvider(archive ports.ArchiveReader, active ports.HistoryManager) ports.UnifiedHistoryProvider {
	return &unifiedProvider{
		archive: archive,
		active:  active,
	}
}

// GetHistoryStream returns a unified, read-only stream of history.
// It prioritizes active memory history and then paginates into the archive.
func (p *unifiedProvider) GetHistoryStream(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
	// If cursor is empty, we start with active history.
	if cursor == "" {
		contents, err := p.active.GetWindow(ctx, 0, -1)
		if err != nil {
			return nil, "", fmt.Errorf("get active history: %w", err)
		}

		var dtos []ports.HistoryViewDTO
		for i, c := range contents {
			if c == nil {
				continue
			}

			// CRITICAL FILTER: Drop auto-summary messages.
			if p.isAutoSummary(c) {
				continue
			}

			dto := p.toDTO(c, false)
			dto.OriginalIndex = i
			dtos = append(dtos, dto)
		}

		// After active history, we point to the END of the archive to read backwards.
		return dtos, "archive:-1", nil
	}

	// If cursor points to the archive, we paginate from disk backwards.
	if strings.HasPrefix(cursor, "archive:") {
		offsetStr := strings.TrimPrefix(cursor, "archive:")
		offset, err := strconv.ParseInt(offsetStr, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid archive cursor %q: %w", cursor, err)
		}

		if offset == 0 {
			return nil, "EOF", nil
		}

		dtos, nextOffset, err := p.archive.ReadPrevious(ctx, limit, offset)
		if err != nil {
			return nil, "", fmt.Errorf("read archive page: %w", err)
		}

		if len(dtos) == 0 || nextOffset == 0 {
			return dtos, "EOF", nil
		}

		nextCursor := fmt.Sprintf("archive:%d", nextOffset)
		return dtos, nextCursor, nil
	}

	return nil, "", fmt.Errorf("unsupported cursor format: %s", cursor)
}

func (p *unifiedProvider) isAutoSummary(c *llm.Content) bool {
	if c.Role != "system" {
		return false
	}
	for _, part := range c.Parts {
		if strings.Contains(part.Text, "System Auto-Summary:") {
			return true
		}
	}
	return false
}

func (p *unifiedProvider) toDTO(c *llm.Content, archived bool) ports.HistoryViewDTO {
	dto := ports.HistoryViewDTO{
		Role:       c.Role,
		IsArchived: archived,
		IsPinned:   c.Pinned,
	}

	var preview strings.Builder
	var thought strings.Builder
	var toolCalls []string

	for _, part := range c.Parts {
		if part == nil {
			continue
		}

		if part.IsThought {
			thought.WriteString(part.Text)
			continue
		}

		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, part.FunctionCall.Name)
		}
		if part.FunctionResponse != nil {
			toolCalls = append(toolCalls, part.FunctionResponse.Name)
		}

		if part.Text != "" {
			preview.WriteString(part.Text)
		}

		// Mask large binary data
		if part.InlineData != nil && len(part.InlineData.Data) > 0 {
			preview.WriteString(" [Image Attached] ")
		}

		if part.AssetID != "" {
			fmt.Fprintf(&preview, " [Attached Asset: %s] ", part.AssetID)
		}
	}

	dto.ContentPreview = strings.TrimSpace(preview.String())
	dto.ThoughtProcess = strings.TrimSpace(thought.String())
	dto.ToolCalls = toolCalls

	return dto
}
