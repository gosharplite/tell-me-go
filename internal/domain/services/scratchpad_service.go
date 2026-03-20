// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

const scratchpadKey = "content"

// scratchpadService handles the logic for managing the scratchpad.
type scratchpadService struct {
	mu         sync.RWMutex
	store      ports.KVStore
	scratchpad string
}

// NewScratchpadService creates a new scratchpadService.
func NewScratchpadService(store ports.KVStore) ports.ScratchpadService {
	return &scratchpadService{
		store: store,
	}
}

// Initialize loads the scratchpad from the repository.
func (s *scratchpadService) Initialize(ctx context.Context) error {
	content, err := s.store.Get(ctx, scratchpadKey)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.scratchpad = content
	s.mu.Unlock()
	return nil
}

// Read returns the current scratchpad content.
func (s *scratchpadService) Read() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scratchpad
}

// Write overwrites the scratchpad content.
func (s *scratchpadService) Write(ctx context.Context, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.Set(ctx, scratchpadKey, content); err != nil {
		return err
	}

	s.scratchpad = content
	return nil
}

// Append adds content to the scratchpad.
func (s *scratchpadService) Append(ctx context.Context, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nextState := s.scratchpad
	if nextState != "" {
		nextState += "\n"
	}
	nextState += content

	if err := s.store.Set(ctx, scratchpadKey, nextState); err != nil {
		return err
	}

	s.scratchpad = nextState
	return nil
}

// Clear empties the scratchpad.
func (s *scratchpadService) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.Delete(ctx, scratchpadKey); err != nil {
		return err
	}

	s.scratchpad = ""
	return nil
}
