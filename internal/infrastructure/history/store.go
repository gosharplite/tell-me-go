// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// store defines the interface for history persistence.
type store interface {
	Load(ctx context.Context) ([]*llm.Content, error)
	Save(ctx context.Context, history []*llm.Content) error
	Append(ctx context.Context, contents []*llm.Content) error
	Archive(ctx context.Context, contents []*llm.Content) error
	AppendParts(ctx context.Context, index int, parts []*llm.Part) error
	UpdateMetadata(ctx context.Context, index int, metadata map[string]interface{}) error
	Compact(ctx context.Context) error
	Sync(ctx context.Context) error
}

// historyPatch represents an append-only patch to history.
type historyPatch struct {
	IsPatch     bool                   `json:"_patch"`
	Index       int                    `json:"index"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	AppendParts []*llm.Part            `json:"append_parts,omitempty"`
}

// jsonlStore implements Store using a JSON Lines file.
type jsonlStore struct {
	filePath    string
	archivePath string
	assetStore  *infrapersistence.AssetStore
	fs          persistence.FileSystem
}

// newJSONLStore creates a new jsonlStore.
func newJSONLStore(fs persistence.FileSystem, filePath string, archivePath string) *jsonlStore {
	assetDir := filepath.Join(filepath.Dir(filePath), "assets")
	return &jsonlStore{
		filePath:    filePath,
		archivePath: archivePath,
		assetStore:  infrapersistence.NewAssetStore(fs, assetDir),
		fs:          fs,
	}
}

// withFileSystem sets the filesystem implementation.
func (s *jsonlStore) withFileSystem(fs persistence.FileSystem) *jsonlStore {
	s.fs = fs
	s.assetStore = infrapersistence.NewAssetStore(fs, s.assetStore.GetBaseDir())
	return s
}

// Load reads the history from the JSONL file.
func (s *jsonlStore) Load(ctx context.Context) ([]*llm.Content, error) {
	_ = s.migrateLegacyJSONFile(ctx)

	if _, err := s.fs.Stat(ctx, s.filePath); os.IsNotExist(err) {
		// Fallback: Check if old .json file exists
		if filepath.Ext(s.filePath) == ".jsonl" {
			oldPath := s.filePath[:len(s.filePath)-1] // .jsonl -> .json
			if _, err := s.fs.Stat(ctx, oldPath); err == nil {
				data, readErr := s.fs.ReadFile(ctx, oldPath)
				if readErr == nil {
					return s.loadLegacyJSON(ctx, data)
				}
			}
		}
		// Return ErrHistoryNotFound to allow domain layers to make decisions (like starting fresh)
		return nil, fmt.Errorf("stat history file %s: %w", s.filePath, ports.ErrHistoryNotFound)
	}

	data, err := s.fs.ReadFile(ctx, s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("loading history from %s: %w", s.filePath, ports.ErrHistoryNotFound)
		}
		return nil, fmt.Errorf("reading history file %s: %w", s.filePath, err)
	}

	if len(data) == 0 {
		return []*llm.Content{}, nil
	}

	// Try decoding as a JSON array first
	var contents []*llm.Content
	if data[0] == '[' {
		if err := json.Unmarshal(data, &contents); err == nil {
			return contents, nil
		}
	}

	// Fallback to JSONL format
	return s.loadJSONL(ctx, data)
}

func (s *jsonlStore) loadLegacyJSON(ctx context.Context, data []byte) ([]*llm.Content, error) {
	var contents []*llm.Content
	if err := json.Unmarshal(data, &contents); err != nil {
		return nil, fmt.Errorf("failed to decode legacy JSON: %w", err)
	}
	for _, c := range contents {
		c.Validate()
	}
	return contents, nil
}

func (s *jsonlStore) migrateLegacyJSONFile(ctx context.Context) error {
	// Fallback/Migration: If history.json exists but history.jsonl doesn't, rename it.
	if filepath.Ext(s.filePath) == ".jsonl" {
		oldPath := s.filePath[:len(s.filePath)-1] // .jsonl -> .json
		if _, err := s.fs.Stat(ctx, oldPath); err == nil {
			if _, err := s.fs.Stat(ctx, s.filePath); os.IsNotExist(err) {
				// We don't have an os.Rename in the FileSystem interface, so we read and write.
				data, err := s.fs.ReadFile(ctx, oldPath)
				if err != nil {
					return err
				}

				if err := s.fs.WriteFile(ctx, s.filePath, data, 0644); err != nil {
					return err
				}

				_ = s.fs.Remove(ctx, oldPath)
			}
		}
	}
	return nil
}

func (s *jsonlStore) loadJSONL(ctx context.Context, data []byte) ([]*llm.Content, error) {
	var contents []*llm.Content
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("failed to decode JSONL: %w", err)
		}

		var patch historyPatch
		if err := json.Unmarshal(raw, &patch); err == nil && patch.IsPatch {
			contents = s.applyPatch(patch, contents)
			continue
		}

		content, err := s.parseContent(raw)
		if err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}

	return contents, nil
}

func (s *jsonlStore) applyPatch(patch historyPatch, contents []*llm.Content) []*llm.Content {
	if patch.Index >= 0 && patch.Index < len(contents) {
		if len(patch.AppendParts) > 0 {
			for _, p := range patch.AppendParts {
				if p != nil {
					contents[patch.Index].Parts = append(contents[patch.Index].Parts, p)
				}
			}
		}
		if pinned, ok := patch.Metadata["pinned"]; ok {
			if pinnedBool, ok := pinned.(bool); ok {
				contents[patch.Index].Pinned = pinnedBool
			}
		}
	}
	return contents
}

func (s *jsonlStore) parseContent(raw json.RawMessage) (*llm.Content, error) {
	var content llm.Content
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, fmt.Errorf("failed to decode JSONL content: %w", err)
	}
	content.Validate() // Boundary sanitization
	return &content, nil
}

// Resolve implements llm.AssetResolver.
func (s *jsonlStore) Resolve(ctx context.Context, assetID string) ([]byte, error) {
	return s.assetStore.Get(ctx, assetID)
}

// Save overwrites the entire history file (compaction/snapshot).
func (s *jsonlStore) Save(ctx context.Context, contents []*llm.Content) error {
	dir := filepath.Dir(s.filePath)
	if _, err := s.fs.Stat(ctx, dir); os.IsNotExist(err) {
		if err := s.fs.MkdirAll(ctx, dir, 0755); err != nil {
			return fmt.Errorf("failed to create history directory: %w", err)
		}
	}

	var buf bytes.Buffer
	for _, c := range contents {
		prepared, err := s.prepareForStorage(ctx, c)
		if err != nil {
			return err
		}
		line, err := json.Marshal(prepared)
		if err != nil {
			return fmt.Errorf("failed to marshal content: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	return s.fs.WriteFile(ctx, s.filePath, buf.Bytes(), 0644)
}

// Append appends multiple content entries to the history file.
func (s *jsonlStore) Append(ctx context.Context, contents []*llm.Content) (err error) {
	if len(contents) == 0 {
		return nil
	}

	if err := s.ensureDirectory(ctx); err != nil {
		return err
	}

	f, oerr := s.fs.OpenFile(ctx, s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oerr != nil {
		return oerr
	}
	defer func() {
		if serr := f.Sync(); serr != nil && err == nil {
			err = serr
		}
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	for _, content := range contents {
		if err = s.appendSingleContent(ctx, f, content); err != nil {
			return err
		}
	}
	return nil
}

// Archive appends content entries to the history.archive.jsonl file.
func (s *jsonlStore) Archive(ctx context.Context, contents []*llm.Content) (err error) {
	if len(contents) == 0 {
		return nil
	}

	if err := s.ensureDirectory(ctx); err != nil {
		return err
	}

	f, oerr := s.fs.OpenFile(ctx, s.archivePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oerr != nil {
		return oerr
	}
	defer func() {
		if serr := f.Sync(); serr != nil && err == nil {
			err = serr
		}
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	for _, content := range contents {
		if err = s.appendSingleContent(ctx, f, content); err != nil {
			return err
		}
	}
	return nil
}

func (s *jsonlStore) ensureDirectory(ctx context.Context) error {
	dir := filepath.Dir(s.filePath)
	if _, err := s.fs.Stat(ctx, dir); os.IsNotExist(err) {
		if err := s.fs.MkdirAll(ctx, dir, 0755); err != nil {
			return fmt.Errorf("failed to create history directory: %w", err)
		}
	}
	return nil
}

func (s *jsonlStore) appendSingleContent(ctx context.Context, f persistence.File, content *llm.Content) error {
	prepared, err := s.prepareForStorage(ctx, content)
	if err != nil {
		return err
	}

	line, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if _, err = f.Write(line); err != nil {
		return err
	}
	return nil
}

// prepareForStorage offloads binary data to AssetStore and returns a clone for JSON marshaling.
func (s *jsonlStore) prepareForStorage(ctx context.Context, c *llm.Content) (*llm.Content, error) {
	if c == nil {
		return nil, nil
	}

	// Use the existing deep clone implementation
	clone := llm.CloneContent(c)

	for _, p := range clone.Parts {
		if p == nil || p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}

		id, err := s.assetStore.Put(ctx, p.InlineData.Data)
		if err != nil {
			return nil, err
		}
		p.AssetID = id
		// Null out data in the storage clone to save space
		p.InlineData.Data = nil
	}

	return clone, nil
}

// UpdateMetadata appends a patch to update metadata of an existing entry.
func (s *jsonlStore) UpdateMetadata(ctx context.Context, index int, metadata map[string]interface{}) (err error) {
	if err := s.ensureDirectory(ctx); err != nil {
		return err
	}
	f, oerr := s.fs.OpenFile(ctx, s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oerr != nil {
		return oerr
	}
	defer func() {
		if serr := f.Sync(); serr != nil && err == nil {
			err = serr
		}
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	patch := historyPatch{
		IsPatch:  true,
		Index:    index,
		Metadata: metadata,
	}
	line, merr := json.Marshal(patch)
	if merr != nil {
		return merr
	}
	line = append(line, '\n')

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err = f.Write(line)
	return err
}

// Compact reads the patched history and overwrites the file without patches.
func (s *jsonlStore) Compact(ctx context.Context) error {
	contents, err := s.Load(ctx)
	if err != nil {
		return err
	}
	return s.Save(ctx, contents)
}

// AppendParts appends a patch to add parts to an existing entry.
func (s *jsonlStore) AppendParts(ctx context.Context, index int, parts []*llm.Part) (err error) {
	if err := s.ensureDirectory(ctx); err != nil {
		return err
	}
	f, oerr := s.fs.OpenFile(ctx, s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oerr != nil {
		return oerr
	}
	defer func() {
		if serr := f.Sync(); serr != nil && err == nil {
			err = serr
		}
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	preparedParts := make([]*llm.Part, len(parts))
	for i, p := range parts {
		dummy := &llm.Content{Parts: []*llm.Part{p}}
		prepared, err := s.prepareForStorage(ctx, dummy)
		if err != nil {
			return err
		}
		preparedParts[i] = prepared.Parts[0]
	}

	patch := historyPatch{
		IsPatch:     true,
		Index:       index,
		AppendParts: preparedParts,
	}
	line, merr := json.Marshal(patch)
	if merr != nil {
		return merr
	}
	line = append(line, '\n')

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err = f.Write(line)
	return err
}

// Sync ensures the history file is synchronized to disk.
func (s *jsonlStore) Sync(ctx context.Context) error {
	f, err := s.fs.OpenFile(ctx, s.filePath, os.O_RDWR, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	return f.Sync()
}
