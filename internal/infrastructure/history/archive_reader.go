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

// toDTO converts an archived llm.Content into a HistoryViewDTO.
//
// Note: llm.Content currently lacks ID and Timestamp in its base struct.
// These would be zero-valued unless the persistence layer is extended.
func (r *jsonlArchiveReader) toDTO(content llm.Content) ports.HistoryViewDTO {
	agg := newPartAggregator()
	for _, part := range content.Parts {
		agg.absorb(part)
	}
	return ports.HistoryViewDTO{
		Role:           content.Role,
		IsArchived:     true,
		ContentPreview: strings.TrimSpace(agg.preview.String()),
		ThoughtProcess: strings.TrimSpace(agg.thought.String()),
		ToolCalls:      agg.toolCalls,
	}
}

// partAggregator accumulates display-relevant data from llm.Part instances
// for archived history rendering.
type partAggregator struct {
	preview   strings.Builder
	thought   strings.Builder
	toolCalls []string
}

func newPartAggregator() *partAggregator { return &partAggregator{} }

// absorb folds a single part into the aggregator. Nil parts are ignored.
// Thought parts contribute only to the thought buffer; all other parts may
// contribute tool names and/or preview text.
func (a *partAggregator) absorb(part *llm.Part) {
	if part == nil {
		return
	}
	if part.IsThought {
		a.thought.WriteString(part.Text)
		return
	}
	a.collectToolNames(part)
	a.collectPreview(part)
}

func (a *partAggregator) collectToolNames(part *llm.Part) {
	if part.FunctionCall != nil {
		a.toolCalls = append(a.toolCalls, part.FunctionCall.Name)
	}
	if part.FunctionResponse != nil {
		a.toolCalls = append(a.toolCalls, part.FunctionResponse.Name)
	}
}

// collectPreview appends preview text and mask placeholders for binary assets.
func (a *partAggregator) collectPreview(part *llm.Part) {
	if part.Text != "" {
		a.preview.WriteString(part.Text)
	}
	// Mask large binary data
	if part.InlineData != nil && len(part.InlineData.Data) > 0 {
		a.preview.WriteString(" [Attached Image] ")
	}
	if part.AssetID != "" {
		a.preview.WriteString(" [Attached Asset] ")
	}
}
