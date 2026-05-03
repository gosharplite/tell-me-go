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

// cursorState represents the decoded pagination cursor.
type cursorState struct {
	isArchive bool
	offset    int64
}

// GetHistoryStream returns a unified, read-only stream of history.
// It prioritizes active memory history and then paginates into the archive.
func (p *unifiedProvider) GetHistoryStream(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
	state, err := p.parseCursor(cursor)
	if err != nil {
		return nil, "", err
	}

	if !state.isArchive {
		return p.fetchActiveHistory(ctx)
	}

	return p.fetchArchiveHistory(ctx, limit, state.offset)
}

func (p *unifiedProvider) parseCursor(cursor string) (*cursorState, error) {
	if cursor == "" {
		return &cursorState{isArchive: false}, nil
	}

	if !strings.HasPrefix(cursor, "archive:") {
		return nil, fmt.Errorf("unsupported cursor format: %s", cursor)
	}

	offsetStr := strings.TrimPrefix(cursor, "archive:")
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid archive cursor %q: %w", cursor, err)
	}

	return &cursorState{isArchive: true, offset: offset}, nil
}

func (p *unifiedProvider) encodeNextCursor(nextOffset int64, items []ports.HistoryViewDTO) string {
	if len(items) == 0 || nextOffset == 0 {
		return "EOF"
	}
	return fmt.Sprintf("archive:%d", nextOffset)
}

func (p *unifiedProvider) fetchActiveHistory(ctx context.Context) ([]ports.HistoryViewDTO, string, error) {
	contents, err := p.active.GetWindow(ctx, 0, -1)
	if err != nil {
		return nil, "", fmt.Errorf("get active history: %w", err)
	}

	var dtos []ports.HistoryViewDTO
	for i, c := range contents {
		if c == nil {
			continue
		}

		dto := p.toDTO(c, false)
		dto.OriginalIndex = i
		dtos = p.processHistoryItem(dto, true, dtos)
	}

	// After active history, we point to the END of the archive to read backwards.
	return dtos, "archive:-1", nil
}

func (p *unifiedProvider) fetchArchiveHistory(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, string, error) {
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

	var filtered []ports.HistoryViewDTO
	for _, dto := range dtos {
		filtered = p.processHistoryItem(dto, true, filtered)
	}

	return filtered, p.encodeNextCursor(nextOffset, dtos), nil
}

func (p *unifiedProvider) isAutoSummary(dto ports.HistoryViewDTO) bool {
	// Filter both the injected summary block and the agent's synthetic acknowledgment
	return strings.Contains(dto.ContentPreview, "System Auto-Summary") ||
		dto.ContentPreview == "Understood. Context compressed."
}

func (p *unifiedProvider) processHistoryItem(dto ports.HistoryViewDTO, skipSummaries bool, results []ports.HistoryViewDTO) []ports.HistoryViewDTO {
	if skipSummaries && p.isAutoSummary(dto) {
		return results
	}
	return append(results, dto)
}

// collectToolCalls appends function call/response names from a part to the toolCalls slice.
func collectToolCalls(part *llm.Part, toolCalls *[]string) {
	if part.FunctionCall != nil {
		*toolCalls = append(*toolCalls, part.FunctionCall.Name)
	}
	if part.FunctionResponse != nil {
		*toolCalls = append(*toolCalls, part.FunctionResponse.Name)
	}
}

// writePartPreview appends text, inline-data markers, and asset markers from a part
// to the preview builder.
func writePartPreview(part *llm.Part, preview *strings.Builder) {
	if part.Text != "" {
		preview.WriteString(part.Text)
	}

	// Mask large binary data
	if part.InlineData != nil && len(part.InlineData.Data) > 0 {
		preview.WriteString(" [Image Attached] ")
	}

	if part.AssetID != "" {
		fmt.Fprintf(preview, " [Attached Asset: %s] ", part.AssetID)
	}
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

		collectToolCalls(part, &toolCalls)
		writePartPreview(part, &preview)
	}

	dto.ContentPreview = strings.TrimSpace(preview.String())
	dto.ThoughtProcess = strings.TrimSpace(thought.String())
	dto.ToolCalls = toolCalls

	return dto
}
