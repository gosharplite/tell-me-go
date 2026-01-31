// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// Store defines the interface for history persistence.
type Store interface {
	Load() ([]*types.Content, error)
	Save(contents []*types.Content) error
	Append(content *types.Content) error
}

// JSONLStore implements Store using a JSON Lines file.
type JSONLStore struct {
	filePath   string
	assetStore *fsutil.AssetStore
}

// NewJSONLStore creates a new JSONLStore.
func NewJSONLStore(filePath string) *JSONLStore {
	assetDir := filepath.Join(filepath.Dir(filePath), "assets")
	return &JSONLStore{
		filePath:   filePath,
		assetStore: fsutil.NewAssetStore(assetDir),
	}
}

// Load reads the history from the JSONL file.
func (s *JSONLStore) Load() ([]*types.Content, error) {
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return []*types.Content{}, nil
	}

	f, err := os.Open(s.filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var contents []*types.Content
	decoder := json.NewDecoder(f)
	for decoder.More() {
		var content types.Content
		if err := decoder.Decode(&content); err != nil {
			return nil, fmt.Errorf("failed to decode JSONL: %w", err)
		}

		contents = append(contents, &content)
	}

	return contents, nil
}

// Resolve implements types.AssetResolver.
func (s *JSONLStore) Resolve(assetID string) ([]byte, error) {
	return s.assetStore.Get(assetID)
}

// Save overwrites the entire history file (compaction/snapshot).
func (s *JSONLStore) Save(contents []*types.Content) error {
	var data []byte
	for _, c := range contents {
		line, err := json.Marshal(s.prepareForStorage(c))
		if err != nil {
			return fmt.Errorf("failed to marshal content: %w", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}

	return fsutil.AtomicWrite(s.filePath, data, 0644)
}

// Append appends a single content entry to the history file.
func (s *JSONLStore) Append(content *types.Content) error {
	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(s.prepareForStorage(content))
	if err != nil {
		return err
	}
	line = append(line, '\n')

	_, err = f.Write(line)
	return err
}

// prepareForStorage offloads binary data to AssetStore and returns a shallow clone for JSON marshaling.
func (s *JSONLStore) prepareForStorage(c *types.Content) *types.Content {
	if c == nil {
		return nil
	}

	clone := &types.Content{
		Role:  c.Role,
		Parts: make([]*types.Part, len(c.Parts)),
	}

	for i, p := range c.Parts {
		pClone := *p // Shallow copy

		if p.InlineData != nil && len(p.InlineData.Data) > 0 {
			id, err := s.assetStore.Put(p.InlineData.Data)
			if err == nil {
				pClone.AssetID = id
				// Null out data in the storage clone to save space
				dataLessBlob := *p.InlineData
				dataLessBlob.Data = nil
				pClone.InlineData = &dataLessBlob
			}
		}
		clone.Parts[i] = &pClone
	}

	return clone
}
