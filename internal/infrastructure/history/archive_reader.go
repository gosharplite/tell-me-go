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

// readLineForIndex reads one complete line from the reader, handling lines
// that exceed the buffer size. It returns the line, its length, whether EOF
// was reached with no data, and any error that is not bufio.ErrBufferFull.
func readLineForIndex(reader *bufio.Reader) (line []byte, lineLen int, eofNoData bool, err error) {
	for {
		chunk, readErr := reader.ReadSlice('\n')
		lineLen += len(chunk)
		line = append(line, chunk...)
		if readErr != nil {
			if readErr == bufio.ErrBufferFull {
				continue
			}
			if readErr == io.EOF {
				if len(line) == 0 {
					return nil, 0, true, nil
				}
				return line, lineLen, false, nil
			}
			return nil, 0, false, fmt.Errorf("read during indexing: %w", readErr)
		}
		return line, lineLen, false, nil
	}
}

// openForIndex opens the archive file for index building.
// Returns (file, nil) on success. Returns (nil, nil) when the file does not
// exist (index is already set to empty). Returns (nil, err) on other errors.
func (r *jsonlArchiveReader) openForIndex(ctx context.Context) (persistence.File, error) {
	file, err := r.fs.Open(ctx, r.archivePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.index = []int64{}
			return nil, nil
		}
		return nil, fmt.Errorf("open archive for indexing %s: %w", r.archivePath, err)
	}
	return file, nil
}

func (r *jsonlArchiveReader) buildIndex(ctx context.Context) error {
	file, err := r.openForIndex(ctx)
	if file == nil {
		return err
	}
	defer func() { _ = file.Close() }()

	var index []int64
	var offset int64
	reader := bufio.NewReader(file)
	for {
		index = append(index, offset)
		_, lineLen, eofNoData, err := readLineForIndex(reader)
		offset += int64(lineLen)
		if err != nil {
			return err
		}
		if eofNoData {
			index = index[:len(index)-1]
			break
		}
	}
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

	return r.readLines(ctx, limit, offset, file)
}

// readLines reads up to limit DTOs from the open file starting at offset.
func (r *jsonlArchiveReader) readLines(ctx context.Context, limit int, offset int64, file persistence.File) ([]ports.HistoryViewDTO, int64, error) {
	// Use SectionReader which uses ReadAt internally, ensuring thread-safety
	// even if 'file' were somehow shared (though here it's local).
	// We use a very large value for max size as we read until EOF or limit.
	section := io.NewSectionReader(file, offset, 1<<62)
	reader := bufio.NewReader(section)
	currentOffset := offset

	var dtos []ports.HistoryViewDTO
	for len(dtos) < limit {
		if err := r.checkContext(ctx); err != nil {
			return nil, 0, err
		}

		lineDtos, lineLen, done, err := r.readAndProcessLine(reader)
		currentOffset += lineLen
		dtos = append(dtos, lineDtos...)
		if done {
			break
		}
		if err != nil {
			return nil, 0, err
		}
	}
	return dtos, currentOffset, nil
}

// checkContext returns ctx.Err() if the context is done, nil otherwise.
func (r *jsonlArchiveReader) checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// readAndProcessLine reads a single JSONL line from reader and converts it.
// It returns any valid DTO found, the byte length consumed, whether EOF was
// reached (done), and any non-EOF error.
func (r *jsonlArchiveReader) readAndProcessLine(reader *bufio.Reader) ([]ports.HistoryViewDTO, int64, bool, error) {
	line, err := reader.ReadBytes('\n')
	lineLen := int64(len(line))

	done := err == io.EOF
	if done {
		err = nil // EOF is a termination signal, not an error
	}

	var dtos []ports.HistoryViewDTO
	if len(line) > 0 {
		if content, ok := r.processArchiveLine(line); ok {
			dtos = append(dtos, r.toDTO(content))
		}
	}

	return dtos, lineLen, done, err
}

// processArchiveLine attempts to decode a JSONL line into a content entry.
// Returns the content and true on success, or zero-value content and false on failure.
// This intentionally skips malformed JSON lines (archives may contain corruption).
func (r *jsonlArchiveReader) processArchiveLine(line []byte) (llm.Content, bool) {
	var content llm.Content
	if err := json.Unmarshal(line, &content); err != nil {
		return llm.Content{}, false
	}
	return content, true
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
