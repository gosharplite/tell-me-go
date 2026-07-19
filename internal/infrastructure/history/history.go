// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence.
package history

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Manager handles loading, saving, and append-only log compaction of conversation history,
// ensuring O(1) in-memory state scaling and efficient persistent log rotation.
type Manager struct {
	mu       sync.RWMutex
	store    store
	logger   ports.Logger
	FilePath string
	Contents []*llm.Content
}

// NewManager creates a new history manager for the given file path.
func NewManager(fs persistence.FileSystem, filePath string, archivePath string) *Manager {
	return &Manager{
		store:    newJSONLStore(fs, filePath, archivePath),
		logger:   &ports.NoOpLogger{},
		FilePath: filePath,
		Contents: []*llm.Content{},
	}
}

// WithLogger sets the logger for the Manager.
func (m *Manager) WithLogger(l ports.Logger) *Manager {
	if l != nil {
		m.logger = l
	}
	return m
}

// setStore allows injecting a custom store.
func (m *Manager) setStore(s store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = s
}

// withFileSystem sets the filesystem implementation for the default store.
func (m *Manager) withFileSystem(fs persistence.FileSystem) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.store.(*jsonlStore); ok {
		s.withFileSystem(fs)
	}
	return m
}

// Load reads the history from the file system.
func (m *Manager) Load(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	contents, err := m.store.Load(ctx)
	if err != nil {
		if errors.Is(err, ports.ErrHistoryNotFound) {
			m.Contents = []*llm.Content{}
			return nil
		}
		return err
	}
	m.Contents = contents

	backfillCount := 0
	for _, c := range m.Contents {
		if c.ID == "" {
			c.ID = llm.NewID()
			backfillCount++
		}
	}
	if backfillCount > 0 {
		if saveErr := m.store.Save(ctx, m.Contents); saveErr != nil {
			m.logger.Warn("failed to persist backfilled UUIDs; continuing with in-memory IDs", "error", saveErr)
		}
	}

	return nil
}

// Save writes the current history to the file system atomically.
func (m *Manager) Save(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.Save(ctx, m.Contents); err != nil {
		return err
	}
	return m.Sync(ctx)
}

// Sync ensures all buffered data is persisted to the physical disk.
func (m *Manager) Sync(ctx context.Context) error {
	return m.store.Sync(ctx)
}

// Archive appends content entries to the archive file.
func (m *Manager) Archive(ctx context.Context, contents []*llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Archive(ctx, contents)
}

// AddContent appends a full api.Content object to the history.
// Note: It does NOT validate role alternation or clean content;
// these are responsibilities of the Orchestration layer.
func (m *Manager) AddContent(ctx context.Context, content *llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cloned := m.clonePersistentContentLocked(content)
	if err := m.store.Append(ctx, []*llm.Content{cloned}); err != nil {
		return err
	}
	m.Contents = append(m.Contents, cloned)
	return nil
}

// GetTotalEntries returns the total number of content entries currently stored.
func (m *Manager) GetTotalEntries() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Contents)
}

// GetWindow returns a deep copy of a specific range of history.
// If endIdx is -1, it returns from startIdx to the end of the history.
func (m *Manager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.Contents)

	if startIdx < 0 {
		return nil, fmt.Errorf("invalid startIdx: %d", startIdx)
	}
	if startIdx > total {
		startIdx = total
	}

	if endIdx == -1 || endIdx > total {
		endIdx = total
	}
	if endIdx < startIdx {
		return nil, fmt.Errorf("invalid range: startIdx=%d, endIdx=%d", startIdx, endIdx)
	}

	window := m.Contents[startIdx:endIdx]
	cloned := make([]*llm.Content, len(window))
	for i, c := range window {
		cloned[i] = llm.CloneContent(c)
	}

	return cloned, nil
}

// SetContents replaces the entire history and persists it to disk.
func (m *Manager) SetContents(ctx context.Context, contents []*llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newContents := make([]*llm.Content, len(contents))
	for i, c := range contents {
		newContents[i] = m.clonePersistentContentLocked(c)
	}

	if err := m.store.Save(ctx, newContents); err != nil {
		return err
	}
	m.Contents = newContents
	return nil
}

func (m *Manager) clonePersistentContentLocked(c *llm.Content) *llm.Content {
	cloned := llm.CloneContent(c)
	if cloned != nil {
		cloned.TransientParts = nil
	}
	return cloned
}

// GetFilePath returns the file path of the history file.
func (m *Manager) GetFilePath() string {
	return m.FilePath
}

// GetResolver returns an AssetResolver from the underlying store.
func (m *Manager) GetResolver() llm.AssetResolver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.store.(llm.AssetResolver); ok {
		return r
	}
	return nil
}

// SetPinned toggles the pinned status of both messages in a turn,
// identified by the stable UUID of either message in the pair.
func (m *Manager) SetPinned(ctx context.Context, turnID string, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find the message with matching ID.
	i := -1
	for idx, c := range m.Contents {
		if c.ID == turnID {
			i = idx
			break
		}
	}
	if i == -1 {
		return fmt.Errorf("turn not found: %s", turnID)
	}

	first, second, err := findTurnPair(m.Contents, i)
	if err != nil {
		return err
	}

	metadata := map[string]interface{}{"pinned": pinned}
	if err := m.store.UpdateMetadata(ctx, first, metadata); err != nil {
		return err
	}
	if err := m.store.UpdateMetadata(ctx, second, metadata); err != nil {
		return err
	}

	m.Contents[first].Pinned = pinned
	m.Contents[second].Pinned = pinned

	return nil
}

// turnPairRule describes how to find the partner message for a turn,
// given the role and an optional content predicate.
type turnPairRule struct {
	role        string
	pred        func(*llm.Content) bool // nil means match any content with this role
	forward     bool                    // true = partner is at i+1, false = partner is at i-1
	partnerRole string                  // expected role of the partner
}

var turnPairRules = []turnPairRule{
	{role: "model", pred: contentHasFunctionCall, forward: true, partnerRole: "user"},
	{role: "user", pred: contentHasFunctionResponse, forward: false, partnerRole: "model"},
	{role: "user", pred: nil, forward: true, partnerRole: "model"},
	{role: "model", pred: nil, forward: false, partnerRole: "user"},
}

// validateTurnPair checks that the partner at the expected index exists,
// has the expected role, and (for tool-call turns) has the correct content type.
func validateTurnPair(contents []*llm.Content, i int, rule turnPairRule) (first, second int, err error) {
	total := len(contents)

	var partnerIdx int
	if rule.forward {
		if i+1 >= total {
			return 0, 0, fmt.Errorf("invalid turn pair: %s at index %d has no following message", rule.role, i)
		}
		partnerIdx = i + 1
	} else {
		if i-1 < 0 {
			return 0, 0, fmt.Errorf("invalid turn pair: %s at index %d has no preceding message", rule.role, i)
		}
		partnerIdx = i - 1
	}

	partner := contents[partnerIdx]
	if partner.Role != rule.partnerRole {
		return 0, 0, fmt.Errorf("invalid turn pair: %s at index %d has unexpected partner role %q", rule.role, i, partner.Role)
	}

	// For tool-call turns, verify the partner has the complementary content type.
	if rule.pred != nil {
		expectedPred := complementPred(rule.role)
		if !expectedPred(partner) {
			return 0, 0, fmt.Errorf("invalid turn pair: %s at index %d has mismatched partner content", rule.role, i)
		}
	}

	if rule.forward {
		return i, partnerIdx, nil
	}
	return partnerIdx, i, nil
}

// complementPred returns the predicate that the partner must satisfy
// for a tool-call turn pair to be valid.
func complementPred(role string) func(*llm.Content) bool {
	if role == "model" {
		return contentHasFunctionResponse
	}
	return contentHasFunctionCall
}

// findTurnPair determines the two indices forming the turn containing
// the message at index i. It handles normal user→model pairs and
// tool-call model→user (FunctionCall/FunctionResponse) pairs.
func findTurnPair(contents []*llm.Content, i int) (first, second int, err error) {
	role := contents[i].Role

	// System messages are not part of any turn.
	if role == "system" {
		return 0, 0, fmt.Errorf("invalid role for turn pairing: %s", role)
	}

	for _, rule := range turnPairRules {
		if rule.role != role {
			continue
		}
		if rule.pred != nil && !rule.pred(contents[i]) {
			continue
		}
		return validateTurnPair(contents, i, rule)
	}

	return 0, 0, fmt.Errorf("invalid role for turn pairing: %s", role)
}

// contentHasFunctionCall returns true if any part of the content contains a FunctionCall.
func contentHasFunctionCall(c *llm.Content) bool {
	for _, p := range c.Parts {
		if p.FunctionCall != nil {
			return true
		}
	}
	return false
}

// contentHasFunctionResponse returns true if any part of the content contains a FunctionResponse.
func contentHasFunctionResponse(c *llm.Content) bool {
	for _, p := range c.Parts {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

// addEntry appends a new text message to the history.
func (m *Manager) addEntry(ctx context.Context, role, text string) error {
	return m.AddContent(ctx, &llm.Content{
		Role:  role,
		ID:    llm.NewID(),
		Parts: []*llm.Part{{Text: text}},
	})
}

// AppendParts appends parts to an existing content entry at the specified index.
func (m *Manager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.Contents) {
		return fmt.Errorf("invalid index: %d", index)
	}

	clonedParts := make([]*llm.Part, len(parts))
	for i, p := range parts {
		dummy := llm.CloneContent(&llm.Content{Parts: []*llm.Part{p}})
		clonedParts[i] = dummy.Parts[0]
	}

	if err := m.store.AppendParts(ctx, index, clonedParts); err != nil {
		return err
	}
	m.Contents[index].Parts = append(m.Contents[index].Parts, clonedParts...)
	return nil
}

// RollbackTurns removes the last N turns (1 turn = 2 messages) from the history.
// It returns the actual number of turns removed, the remaining turns, the remaining total messages, and any error.
func (m *Manager) RollbackTurns(ctx context.Context, turns int) (actualRemoved int, remainingTurns int, remainingMsgs int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	originalLen := len(m.Contents)
	hasSystem := originalLen > 0 && m.Contents[0].Role == "system"

	actualRemoved, newLen := calculateRollbackBounds(originalLen, turns, hasSystem)

	if newLen == originalLen && actualRemoved == 0 {
		return 0, rollbackRemainingTurns(originalLen, hasSystem), originalLen, nil
	}

	tempContents := m.Contents[:newLen]
	if err := m.commitRollback(ctx, tempContents, newLen, originalLen); err != nil {
		return 0, 0, 0, err
	}

	remainingMsgs = len(m.Contents)
	remainingTurns = rollbackRemainingTurns(remainingMsgs, hasSystem)

	return actualRemoved, remainingTurns, remainingMsgs, nil
}

func calculateRollbackBounds(originalLen int, turns int, hasSystem bool) (actualRemoved, newLen int) {
	if originalLen <= 0 {
		return 0, 0
	}

	offset := 0
	if hasSystem {
		offset = 1
	}
	effectiveLen := originalLen - offset

	if turns <= 0 {
		// Invariant: Rollback must result in an even number of effective messages (complete pairs).
		// If the initial state is odd (effective), we always drop the trailing partial turn even if turns=0.
		if effectiveLen%2 != 0 {
			return 0, originalLen - 1
		}
		return 0, originalLen
	}

	currentTurns := (effectiveLen + 1) / 2
	if turns >= currentTurns {
		return currentTurns, offset
	}

	// Calculate how many messages to drop.
	droppedMsgs := turns * 2
	if effectiveLen%2 != 0 {
		droppedMsgs -= 1
	}

	return turns, originalLen - droppedMsgs
}

// commitRollback persists the truncated contents and nils out removed entries.
// It follows durability-first semantics: persist succeeds before memory is mutated.
func (m *Manager) commitRollback(ctx context.Context, tempContents []*llm.Content, newLen, originalLen int) error {
	if err := m.store.Save(ctx, tempContents); err != nil {
		return fmt.Errorf("failed to persist rollback: %w", err)
	}

	// Persisted successfully, now safe to modify memory
	for i := newLen; i < originalLen; i++ {
		m.Contents[i] = nil
	}

	if newLen == 0 {
		m.Contents = nil
	} else {
		m.Contents = tempContents
	}
	return nil
}

// rollbackRemainingTurns computes the number of remaining turns after rollback.
func rollbackRemainingTurns(remainingMsgs int, hasSystem bool) int {
	effectiveLen := remainingMsgs
	if hasSystem {
		effectiveLen--
	}
	return effectiveLen / 2
}

// GetLastModelTurn returns the index and a deep copy of the last model-role
// Content entry. It returns ports.ErrHistoryNotFound if no model turns exist.
func (m *Manager) GetLastModelTurn(ctx context.Context) (int, *llm.Content, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := len(m.Contents) - 1; i >= 0; i-- {
		if m.Contents[i].Role == "model" {
			return i, llm.CloneContent(m.Contents[i]), nil
		}
	}
	return 0, nil, ports.ErrHistoryNotFound
}

// UpdateTurnContent replaces the text and thought parts of the Content at
// the given index, then persists via Save. The index must reference a
// model-role entry. An empty newThought removes any thought part.
func (m *Manager) UpdateTurnContent(ctx context.Context, index int, newText string, newThought string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.Contents) {
		return fmt.Errorf("index %d out of bounds [0, %d)", index, len(m.Contents))
	}
	if m.Contents[index].Role != "model" {
		return fmt.Errorf("index %d has role %q, expected \"model\"", index, m.Contents[index].Role)
	}

	// Collect new parts: keep non-text non-thought parts (e.g., function calls)
	// but replace text and thought.
	var newParts []*llm.Part
	textSet := false
	for _, p := range m.Contents[index].Parts {
		if p.IsThought {
			continue // thought parts are replaced below
		}
		if p.Text != "" && !p.IsThought && !textSet {
			textSet = true
			// Only add the new text part if it is non-empty.
			// Anthropic and other providers reject empty text blocks.
			if newText != "" {
				newParts = append(newParts, &llm.Part{Text: newText})
			}
			continue
		}
		// Keep function calls, function responses, inline data, etc.
		newParts = append(newParts, p)
	}
	// If there was no existing text part, add one
	if !textSet && newText != "" {
		newParts = append(newParts, &llm.Part{Text: newText})
	}

	// Add thought part if requested
	if newThought != "" {
		newParts = append(newParts, &llm.Part{Text: newThought, IsThought: true})
	}

	m.Contents[index].Parts = newParts

	if err := m.store.Save(ctx, m.Contents); err != nil {
		return fmt.Errorf("save after updating turn %d: %w", index, err)
	}
	return nil
}

// GetLastUserMessage finds the text of the last user message and the number of turns to rollback to remove it and everything after it.
func (m *Manager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastMsgText string
	humanMsgAbsoluteIndex := -1

	total := len(m.Contents)

	for i := total - 1; i >= 0; i-- {
		if m.Contents[i].Role == "user" {
			var textBuilder string
			for _, part := range m.Contents[i].Parts {
				if part.Text != "" {
					textBuilder += part.Text
				}
			}

			if textBuilder != "" {
				lastMsgText = textBuilder
				humanMsgAbsoluteIndex = i
				break
			}
		}
	}

	if humanMsgAbsoluteIndex == -1 {
		return "", 0, errors.New("no previous user message found to retry")
	}

	turnsToRollback := (total - humanMsgAbsoluteIndex + 1) / 2

	return lastMsgText, turnsToRollback, nil
}
