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
	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

// Store defines the interface for history persistence.
type Store interface {
	Load(ctx context.Context) ([]*llm.Content, error)
	Save(ctx context.Context, contents []*llm.Content) error
	Append(ctx context.Context, content *llm.Content) error
}

// JSONLStore implements Store using a JSON Lines file.
type JSONLStore struct {
	filePath   string
	assetStore *fsutil.AssetStore
	fs         fsutil.FileSystem
}

// NewJSONLStore creates a new JSONLStore.
func NewJSONLStore(filePath string) *JSONLStore {
	assetDir := filepath.Join(filepath.Dir(filePath), "assets")
	return &JSONLStore{
		filePath:   filePath,
		assetStore: fsutil.NewAssetStore(assetDir),
		fs:         fsutil.DefaultFileSystem,
	}
}

// WithFileSystem sets the filesystem implementation.
func (s *JSONLStore) WithFileSystem(fs fsutil.FileSystem) *JSONLStore {
	s.fs = fs
	s.assetStore.WithFileSystem(fs)
	return s
}

// Load reads the history from the JSONL file.
func (s *JSONLStore) Load(ctx context.Context) ([]*llm.Content, error) {
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
func (s *JSONLStore) Resolve(ctx context.Context, assetID string) ([]byte, error) {
	return s.assetStore.Get(ctx, assetID)
}

// Save overwrites the entire history file (compaction/snapshot).
func (s *JSONLStore) Save(ctx context.Context, contents []*llm.Content) error {
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

// Append appends a single content entry to the history file.
func (s *JSONLStore) Append(ctx context.Context, content *llm.Content) error {
	f, err := s.fs.OpenFile(ctx, s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

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

	_, err = f.Write(line)
	return err
}

// prepareForStorage offloads binary data to AssetStore and returns a shallow clone for JSON marshaling.
func (s *JSONLStore) prepareForStorage(ctx context.Context, c *llm.Content) (*llm.Content, error) {
	if c == nil {
		return nil, nil
	}

	clone := &llm.Content{
		Role:  c.Role,
		Parts: make([]*llm.Part, len(c.Parts)),
	}

	for i, p := range c.Parts {
		pClone := *p // Shallow copy

		if p.InlineData != nil && len(p.InlineData.Data) > 0 {
			id, err := s.assetStore.Put(ctx, p.InlineData.Data)
			if err != nil {
				return nil, err
			}
			pClone.AssetID = id
			// Null out data in the storage clone to save space
			dataLessBlob := *p.InlineData
			dataLessBlob.Data = nil
			pClone.InlineData = &dataLessBlob
		}
		clone.Parts[i] = &pClone
	}

	return clone, nil
}
