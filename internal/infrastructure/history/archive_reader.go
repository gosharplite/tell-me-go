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
	"sort"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// jsonlArchiveReader implements ports.ArchiveReader using a JSONL file.
type jsonlArchiveReader struct {
	fs          persistence.FileSystem
	archivePath string
	mu          sync.RWMutex
	index       []int64 // offsets of each line
	indexed     bool
}

// NewJSONLArchiveReader creates a new JSONLArchiveReader.
func NewJSONLArchiveReader(fs persistence.FileSystem, archivePath string) ports.ArchiveReader {
	return &jsonlArchiveReader{
		fs:          fs,
		archivePath: archivePath,
	}
}

// ReadPage reads a page of history from the archive file using byte-offset seeking.
func (r *jsonlArchiveReader) ReadPage(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	dtos, nextOffset, err := r.readPageInternal(ctx, limit, offset)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, offset, nil
		}
		return nil, 0, fmt.Errorf("read page from %s at %d: %w", r.archivePath, offset, err)
	}
	return dtos, nextOffset, nil
}

// ReadPrevious reads archived history backwards from a given offset.
// It returns 'limit' entries that precede the offset, in chronological order,
// and the offset of the first entry returned.
func (r *jsonlArchiveReader) ReadPrevious(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	if err := r.ensureIndex(ctx); err != nil {
		return nil, 0, err
	}

	r.mu.RLock()
	if len(r.index) == 0 {
		r.mu.RUnlock()
		return nil, 0, nil
	}

	// Find the current index for the offset
	// If offset is -1 or greater than file size, we start from the last line.
	targetIdx := len(r.index)
	if offset != -1 {
		targetIdx = sort.Search(len(r.index), func(i int) bool {
			return r.index[i] >= offset
		})
	}

	if targetIdx == 0 {
		r.mu.RUnlock()
		return nil, 0, nil
	}

	startIdx := targetIdx - limit
	if startIdx < 0 {
		startIdx = 0
	}

	startOffset := r.index[startIdx]
	limitToRead := targetIdx - startIdx
	r.mu.RUnlock()

	dtos, _, err := r.readPageInternal(ctx, limitToRead, startOffset)
	if err != nil {
		return nil, 0, err
	}

	return dtos, startOffset, nil
}

func (r *jsonlArchiveReader) ensureIndex(ctx context.Context) error {
	r.mu.RLock()
	if r.indexed {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexed {
		return nil
	}

	if err := r.buildIndex(ctx); err != nil {
		return err // Do not cache transient errors
	}
	r.indexed = true
	return nil
}

func (r *jsonlArchiveReader) buildIndex(ctx context.Context) error {
	file, err := r.fs.Open(ctx, r.archivePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.index = []int64{}
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
		for {
			line, err := reader.ReadSlice('\n')
			offset += int64(len(line))
			if err != nil {
				if err == bufio.ErrBufferFull {
					// Line exceeds buffer; we still counted the chunk length correctly.
					// Just clear the error and continue to read the rest of the line.
					continue
				}
				if err == io.EOF {
					if len(line) == 0 {
						index = index[:len(index)-1]
					}
					goto done
				}
				return fmt.Errorf("read during indexing: %w", err)
			}
			break
		}
	}
done:
	r.index = index
	return nil
}

// readPageInternal reads a page of history from the archive file.
// It is thread-safe as it opens its own file handle and uses io.ReaderAt.
func (r *jsonlArchiveReader) readPageInternal(ctx context.Context, limit int, offset int64) ([]ports.HistoryViewDTO, int64, error) {
	file, err := r.fs.Open(ctx, r.archivePath)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()

	// Use SectionReader which uses ReadAt internally, ensuring thread-safety
	// even if 'file' were somehow shared (though here it's local).
	// We use a very large value for max size as we read until EOF or limit.
	section := io.NewSectionReader(file, offset, 1<<62)
	reader := bufio.NewReader(section)
	currentOffset := offset

	var dtos []ports.HistoryViewDTO
	for len(dtos) < limit {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		default:
		}

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
			if err == io.EOF {
				break
			}
			return nil, 0, err
		}
	}
	return dtos, currentOffset, nil
}

func (r *jsonlArchiveReader) toDTO(content llm.Content) ports.HistoryViewDTO {
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
