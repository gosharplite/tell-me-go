// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// JSONLArchiveReader implements ports.ArchiveReader using a JSONL file.
type JSONLArchiveReader struct {
	fs          persistence.FileSystem
	archivePath string
	mu          sync.Mutex
	index       []int64 // offsets of each line
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
		if errors.Is(err, os.ErrNotExist) {
			return nil, offset, nil
		}
		return nil, 0, fmt.Errorf("open archive %s: %w", r.archivePath, err)
	}
	defer func() { _ = file.Close() }()

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

// ReadPrevious reads archived history backwards from a given offset.
// It returns 'limit' entries that precede the offset, in chronological order,
// and the offset of the first entry returned.
func (r *JSONLArchiveReader) ReadPrevious(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	if err := r.ensureIndex(ctx); err != nil {
		return nil, 0, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.index) == 0 {
		return nil, 0, nil
	}

	// Find the current index for the offset
	// If offset is -1 or greater than file size, we start from the last line.
	targetIdx := len(r.index)
	if offset != -1 {
		// Use binary search if index is large, but for now we find it
		for i, off := range r.index {
			if off >= offset {
				targetIdx = i
				break
			}
		}
	}

	if targetIdx == 0 {
		return nil, 0, nil
	}

	startIdx := targetIdx - limit
	if startIdx < 0 {
		startIdx = 0
	}

	startOffset := r.index[startIdx]
	dtos, _, err := r.readPageInternal(ctx, targetIdx-startIdx, startOffset)
	if err != nil {
		return nil, 0, err
	}

	return dtos, startOffset, nil
}

func (r *JSONLArchiveReader) ensureIndex(ctx context.Context) error {
	r.mu.Lock()
	if r.index != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	// Building index
	file, err := r.fs.Open(ctx, r.archivePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.mu.Lock()
			r.index = []int64{}
			r.mu.Unlock()
			return nil
		}
		return fmt.Errorf("open archive for indexing %s: %w", r.archivePath, err)
	}
	defer func() { _ = file.Close() }()

	var index []int64
	var offset int64
	reader := bufio.NewReader(file)
	for {
		index = append(index, offset)
		line, err := reader.ReadBytes('\n')
		offset += int64(len(line))
		if err != nil {
			if err == io.EOF {
				// The last line (if it doesn't end in \n) might have been counted
				// but let's see. ReadBytes returns the data and EOF if it reaches EOF.
				if len(line) == 0 {
					index = index[:len(index)-1]
				}
				break
			}
			return fmt.Errorf("read during indexing: %w", err)
		}
	}

	r.mu.Lock()
	r.index = index
	r.mu.Unlock()
	return nil
}

// readPageInternal is like ReadPage but assumes mu is NOT locked.
func (r *JSONLArchiveReader) readPageInternal(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	file, err := r.fs.Open(ctx, r.archivePath)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err
	}

	var dtos []ports.HistoryViewDTO
	reader := bufio.NewReader(file)
	currentOffset := offset

	for len(dtos) < limit {
		line, err := reader.ReadBytes('\n')
		lineLen := int64(len(line))

		if len(line) > 0 {
			var content llm.Content
			if err := json.Unmarshal(line, &content); err == nil {
				dtos = append(dtos, r.toDTO(content))
			}
			currentOffset += lineLen
		}

		if err != nil {
			break
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
