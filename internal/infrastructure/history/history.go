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

// NewManagerWithAssetStore creates a new history manager with an injected
// asset store (the domain persistence.AssetStore port). The DI root uses this
// to inject the concrete infrastructure AssetStore built against the resolved
// AssetsPath, inverting the adapter-to-adapter edge (issue #1350, item 5).
func NewManagerWithAssetStore(fs persistence.FileSystem, assetStore persistence.AssetStore, filePath string, archivePath string) *Manager {
	return &Manager{
		store:    newJSONLStoreWithAssetStore(fs, assetStore, filePath, archivePath),
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
// If content.ID is empty, a new UUID is auto-generated to ensure every
// entry has a stable identity for operations like SetPinned.
// Note: It does NOT validate role alternation or clean content;
// these are responsibilities of the Orchestration layer.
func (m *Manager) AddContent(ctx context.Context, content *llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cloned := m.clonePersistentContentLocked(content)
	if cloned.ID == "" {
		cloned.ID = llm.NewID()
	}
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

// GetModelTurn returns a deep copy of the model-role Content at the given index.
func (m *Manager) GetModelTurn(ctx context.Context, index int) (*llm.Content, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if index < 0 || index >= len(m.Contents) {
		return nil, ports.ErrHistoryNotFound
	}

	entry := m.Contents[index]
	if entry.Role != "model" {
		return nil, ports.ErrHistoryNotFound
	}

	copied := *entry
	copied.Parts = make([]*llm.Part, len(entry.Parts))
	for i, p := range entry.Parts {
		pc := *p
		copied.Parts[i] = &pc
	}

	return &copied, nil
}

// isNonTextNonThought returns true when a part is neither a thought nor a
// text part (i.e., it's a function call, function response, inline data, etc.).
func isNonTextNonThought(p *llm.Part) bool {
	return !p.IsThought && p.Text == ""
}

// findTextReplacementIndex scans original for the position where newText
// should be inserted. It returns the count of non-text, non-thought parts
// that appear before the first text part. Returns -1 if no text part exists.
func findTextReplacementIndex(original []*llm.Part) int {
	insertIdx := 0
	for _, p := range original {
		if p.IsThought {
			continue
		}
		if p.Text != "" {
			return insertIdx
		}
		insertIdx++
	}
	return -1
}

// insertTextAtPosition inserts a text part into parts at the given index.
// If text is empty, parts is returned unchanged. If idx is out of range,
// the text part is appended.
func insertTextAtPosition(parts []*llm.Part, text string, idx int) []*llm.Part {
	if text == "" {
		return parts
	}
	textPart := &llm.Part{Text: text}
	if idx >= 0 && idx <= len(parts) {
		return append(parts[:idx], append([]*llm.Part{textPart}, parts[idx:]...)...)
	}
	return append(parts, textPart)
}

// rebuildTextParts builds a new Parts slice from original, replacing all text
// parts with newText (inserted once at the first text part position). Non-text,
// non-thought parts (function calls, function responses, inline data) are preserved.
// If original has no text parts and newText is non-empty, newText is appended.
func rebuildTextParts(original []*llm.Part, newText string) []*llm.Part {
	insertIdx := findTextReplacementIndex(original)

	// Keep function calls, function responses, inline data, etc.
	// Drop all text parts. The editor concatenates all text parts
	// into one buffer, so preserving subsequent parts would
	// duplicate content on save.
	var newParts []*llm.Part
	for _, p := range original {
		if isNonTextNonThought(p) {
			newParts = append(newParts, p)
		}
	}

	return insertTextAtPosition(newParts, newText, insertIdx)
}

// findThoughtIndex returns the index of the first thought part in original,
// or -1 if none exists.
func findThoughtIndex(original []*llm.Part) int {
	for i, p := range original {
		if p.IsThought {
			return i
		}
	}
	return -1
}

// insertThoughtIfPresent inserts a thought part into newParts if newThought is
// non-empty. The insertion position mirrors the position of the first thought
// part in original. If original had no thought part, newThought is appended.
// KNOWN LIMITATION: thoughtIdx is computed from original, but newParts may have
// fewer elements because text parts are dropped. For common provider layouts
// this is correct; for exotic layouts it's cosmetic — semantic content is preserved.
func insertThoughtIfPresent(newParts []*llm.Part, original []*llm.Part, newThought string) []*llm.Part {
	// If the original had a thought part, insert newThought at its position.
	// If there was no thought part but newThought is provided, append it.
	if newThought == "" {
		return newParts
	}

	thoughtPart := &llm.Part{Text: newThought, IsThought: true}
	thoughtIdx := findThoughtIndex(original)

	// KNOWN LIMITATION: thoughtIdx is computed from original, but
	// text parts are dropped during the rebuild, so newParts may
	// have fewer elements. For common provider layouts (thought-first
	// or thought-after-a-single-text-part) the index is correct.
	// For exotic layouts like [textA, textB, thought, FC] where both
	// text parts are dropped, the thought inserts earlier than
	// expected. Cosmetic — the semantic content is preserved.
	if thoughtIdx >= 0 && thoughtIdx <= len(newParts) {
		return append(newParts[:thoughtIdx], append([]*llm.Part{thoughtPart}, newParts[thoughtIdx:]...)...)
	}
	return append(newParts, thoughtPart)
}

// collectUpdatedParts builds a new Parts slice by replacing the text and
// thought parts in original with newText and newThought. Non-text,
// non-thought parts (function calls, responses, inline data) are preserved.
// An empty newThought omits the thought part. An empty newText omits the
// text part (the old text part is dropped with no replacement).
// Delegates to rebuildTextParts and insertThoughtIfPresent.
func collectUpdatedParts(original []*llm.Part, newText string, newThought string) []*llm.Part {
	newParts := rebuildTextParts(original, newText)
	newParts = insertThoughtIfPresent(newParts, original, newThought)
	return newParts
}

// UpdateTurnContent replaces the text and thought parts of the Content at
// the given index. The replacement is persisted via Save before memory is
// updated (durability-first). The index must reference a model-role entry.
// An empty newThought removes any thought part.
func (m *Manager) UpdateTurnContent(ctx context.Context, index int, newText string, newThought string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.Contents) {
		return fmt.Errorf("index %d out of bounds [0, %d)", index, len(m.Contents))
	}
	if m.Contents[index].Role != "model" {
		return fmt.Errorf("index %d has role %q, expected \"model\"", index, m.Contents[index].Role)
	}

	newParts := collectUpdatedParts(m.Contents[index].Parts, newText, newThought)
	copyOfContents := make([]*llm.Content, len(m.Contents))
	for i, c := range m.Contents {
		if i == index {
			cc := *c // shallow struct copy — Content has no map fields
			cc.Parts = newParts
			copyOfContents[i] = &cc
		} else {
			copyOfContents[i] = c // safe: Save deep-clones its input
		}
	}
	if err := m.store.Save(ctx, copyOfContents); err != nil {
		return fmt.Errorf("save after updating turn %d: %w", index, err)
	}
	m.Contents[index].Parts = newParts
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
