// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// store defines the interface for history persistence.
type store interface {
	Load(ctx context.Context) ([]*llm.Content, error)
	Save(ctx context.Context, history []*llm.Content) error
	Append(ctx context.Context, contents []*llm.Content) error
}

// jsonlStore implements Store using a JSON Lines file.
type jsonlStore struct {
	filePath   string
	assetStore *storage.AssetStore
	fs         storage.FileSystem
}

// newJSONLStore creates a new jsonlStore.
func newJSONLStore(filePath string) *jsonlStore {
	assetDir := filepath.Join(filepath.Dir(filePath), "assets")
	return &jsonlStore{
		filePath:   filePath,
		assetStore: storage.NewAssetStore(assetDir),
		fs:         storage.DefaultFileSystem,
	}
}

// withFileSystem sets the filesystem implementation.
func (s *jsonlStore) withFileSystem(fs storage.FileSystem) *jsonlStore {
	s.fs = fs
	s.assetStore.WithFileSystem(fs)
	return s
}

// Load reads the history from the JSONL file.
func (s *jsonlStore) Load(ctx context.Context) ([]*llm.Content, error) {
	if _, err := s.fs.Stat(ctx, s.filePath); os.IsNotExist(err) {
		return []*llm.Content{}, nil
	}

	f, err := s.fs.Open(ctx, s.filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var contents []*llm.Content
	decoder := json.NewDecoder(f)
	for decoder.More() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var content llm.Content
		if err := decoder.Decode(&content); err != nil {
			return nil, fmt.Errorf("failed to decode JSONL: %w", err)
		}

		contents = append(contents, &content)
	}

	return contents, nil
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

	var data []byte
	for _, c := range contents {
		prepared, err := s.prepareForStorage(ctx, c)
		if err != nil {
			return err
		}
		line, err := json.Marshal(prepared)
		if err != nil {
			return fmt.Errorf("failed to marshal content: %w", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}

	return s.fs.WriteFile(ctx, s.filePath, data, 0644)
}

// Append appends multiple content entries to the history file.
func (s *jsonlStore) Append(ctx context.Context, contents []*llm.Content) error {
	if len(contents) == 0 {
		return nil
	}

	if err := s.ensureDirectory(ctx); err != nil {
		return err
	}

	f, err := s.fs.OpenFile(ctx, s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, content := range contents {
		if err := s.appendSingleContent(ctx, f, content); err != nil {
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

func (s *jsonlStore) appendSingleContent(ctx context.Context, f storage.File, content *llm.Content) error {
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
