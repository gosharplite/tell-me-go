// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package fsutil

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// AssetStore manages binary blobs in a content-addressable storage.
type AssetStore struct {
	baseDir string
}

// NewAssetStore creates a new AssetStore.
func NewAssetStore(baseDir string) *AssetStore {
	return &AssetStore{baseDir: baseDir}
}

// Put saves the data and returns its SHA-256 hash as the ID.
func (s *AssetStore) Put(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	hash := sha256.Sum256(data)
	id := fmt.Sprintf("%x", hash)

	path := s.GetPath(id)
	if _, err := os.Stat(path); err == nil {
		return id, nil // Already exists
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}

	return id, nil
}

// Get retrieves the data by its ID.
func (s *AssetStore) Get(id string) ([]byte, error) {
	if id == "" {
		return nil, nil
	}
	return os.ReadFile(s.GetPath(id))
}

// GetPath returns the absolute path for an asset ID.
func (s *AssetStore) GetPath(id string) string {
	// Use subdirectories to avoid thousands of files in one folder
	// e.g., <baseDir>/ab/abcdef123...
	if len(id) < 2 {
		return filepath.Join(s.baseDir, id)
	}
	return filepath.Join(s.baseDir, id[:2], id)
}
