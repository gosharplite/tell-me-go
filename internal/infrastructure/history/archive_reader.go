// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// JSONLArchiveReader implements ports.ArchiveReader using a JSONL file.
type JSONLArchiveReader struct {
	fs          persistence.FileSystem
	archivePath string
}

// NewJSONLArchiveReader creates a new JSONLArchiveReader.
func NewJSONLArchiveReader(fs persistence.FileSystem, archivePath string) *JSONLArchiveReader {
	return &JSONLArchiveReader{
		fs:          fs,
		archivePath: archivePath,
	}
}

// ReadPage reads a page of history from the archive file using byte-offset seeking.
func (r *JSONLArchiveReader) ReadPage(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	file, err := r.fs.Open(ctx, r.archivePath)
	if err != nil {
		return nil, 0, fmt.Errorf("open archive %s: %w", r.archivePath, err)
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek to offset %d: %w", offset, err)
	}

	var dtos []ports.HistoryViewDTO
	reader := bufio.NewReader(file)
	currentOffset := offset

	for len(dtos) < limit {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		lineLen := int64(len(line))

		if len(line) > 0 {
			// Basic validation that it's a JSON object
			trimmed := strings.TrimSpace(string(line))
			if len(trimmed) > 0 && trimmed[0] == '{' {
				var content llm.Content
				if err := json.Unmarshal(line, &content); err != nil {
					// If it's a patch, we skip it for now or we could try to handle it.
					// Archived history should generally be full contents.
					// For robustness, we just skip malformed lines in a "read-only view".
				} else {
					dtos = append(dtos, r.toDTO(content))
				}
			}
			currentOffset += lineLen
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, 0, fmt.Errorf("read line at offset %d: %w", currentOffset, err)
		}
	}

	return dtos, currentOffset, nil
}

func (r *JSONLArchiveReader) toDTO(content llm.Content) ports.HistoryViewDTO {
	dto := ports.HistoryViewDTO{
		Role:       content.Role,
		IsArchived: true,
		// Note: llm.Content currently lacks ID and Timestamp in its base struct.
		// These would be zero-valued unless the persistence layer is extended.
	}

	var preview strings.Builder
	var thought strings.Builder
	var toolCalls []string

	for _, part := range content.Parts {
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
			preview.WriteString(" [Attached Image] ")
		}

		if part.AssetID != "" {
			preview.WriteString(" [Attached Asset] ")
		}
	}

	dto.ContentPreview = strings.TrimSpace(preview.String())
	dto.ThoughtProcess = strings.TrimSpace(thought.String())
	dto.ToolCalls = toolCalls

	return dto
}
