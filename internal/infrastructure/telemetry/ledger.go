// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// sessionCostRecord represents a single session's financial footprint.
type sessionCostRecord struct {
	Date      string                    `json:"date"`      // Keep for backward compatibility
	Timestamp time.Time                 `json:"timestamp"` // New source of truth
	Session   string                    `json:"session"`
	Model     string                    `json:"model"`
	TotalCost float64                   `json:"total_cost"`
	Usage     domain_pricing.UsageStats `json:"usage,omitempty"`
}

var (
	recoveryInProgress sync.Map // historyPath -> bool
	dateRegex          = regexp.MustCompile(`(\d{4})[-_/]?(\d{2})[-_/]?(\d{2})`)
	ledgerMu           sync.Mutex
)

const ledgerRecoveryTimeout = 10 * time.Minute

// ledgerStore handles persistence and recovery of cost metrics.
type ledgerStore struct {
	sm               domain_security.ISecurityManager
	model            string
	pricingOverrides map[string]domain_pricing.ModelPricing
}

// newLedgerStore creates a new ledgerStore.
func newLedgerStore(sm domain_security.ISecurityManager, model string, pricingOverrides map[string]domain_pricing.ModelPricing) *ledgerStore {
	return &ledgerStore{
		sm:               sm,
		model:            model,
		pricingOverrides: pricingOverrides,
	}
}

// isStale checks if a file is older than 5 minutes.
func isStale(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return time.Since(info.ModTime()) > 5*time.Minute
	}
	return false
}

// recoverLedger crawls backups and mode directories to reconstruct a missing global_costs.json.
func (ls *ledgerStore) recoverLedger(ctx context.Context, globalDir string) {
	historyPath := filepath.Join(globalDir, "global_costs.json")
	if !ls.tryStartRecovery(historyPath) {
		return
	}
	defer recoveryInProgress.Delete(historyPath)

	seen := ls.loadExistingSessionIDs(historyPath)
	pricing := ls.getPricingWithOverrides(ctx, globalDir)

	files, err := ls.findLogFiles(globalDir)
	if err != nil {
		log.Printf("Recovery: walk failed: %v\n", err)
	}

	discovered := ls.discoverNewRecords(ctx, files, globalDir, seen, pricing)

	if len(discovered) > 0 {
		ls.persistMergedLedger(ctx, historyPath, discovered)
	}
}

// tryStartRecovery attempts to mark the ledger recovery as in-progress.
func (ls *ledgerStore) tryStartRecovery(historyPath string) bool {
	_, loaded := recoveryInProgress.LoadOrStore(historyPath, true)
	return !loaded
}

// loadExistingSessionIDs reads the existing ledger and extracts already processed session IDs.
func (ls *ledgerStore) loadExistingSessionIDs(historyPath string) map[string]bool {
	seen := make(map[string]bool)
	content, err := os.ReadFile(historyPath)
	if err != nil {
		return seen
	}

	var existing []sessionCostRecord
	if err := json.Unmarshal(content, &existing); err != nil {
		log.Printf("Warning: Failed to parse existing ledger during recovery: %v", err)
		return seen
	}

	for _, r := range existing {
		seen[r.Session] = true
	}
	return seen
}

// getPricingWithOverrides fetches the latest pricing data and applies any local overrides.
func (ls *ledgerStore) getPricingWithOverrides(ctx context.Context, globalDir string) domain_pricing.PricingData {
	pricing := GetPricing(ctx, ls.sm, globalDir)
	for k, v := range ls.pricingOverrides {
		pricing.Models[k] = v
	}
	return pricing
}

// discoverNewRecords scans the list of log files for new sessions not yet in the ledger.
func (ls *ledgerStore) discoverNewRecords(ctx context.Context, files []string, globalDir string, seen map[string]bool, pricing domain_pricing.PricingData) []sessionCostRecord {
	var discovered []sessionCostRecord
	for _, path := range files {
		if ctx.Err() != nil {
			break
		}

		// Prevent redundant parsing by checking sessionID first
		sessionID := ls.getSessionID(path, globalDir)
		if seen[sessionID] {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		record, err := ls.processLogFile(path, info, globalDir, pricing)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Recovery: failed to parse %s: %v\n", path, err)
			}
			continue
		}

		if record != nil {
			discovered = append(discovered, *record)
			seen[record.Session] = true
		}
	}
	return discovered
}

func (ls *ledgerStore) findLogFiles(globalDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(globalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			log.Printf("Recovery: error accessing path %q: %v\n", path, err)
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), "tokens.log") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func (ls *ledgerStore) getSessionID(path, globalDir string) string {
	rel, err := filepath.Rel(globalDir, path)
	if err != nil {
		return path // Fallback
	}
	rel = filepath.ToSlash(rel)
	sessionID := rel
	if strings.HasPrefix(rel, "backups/") {
		sessionID = "backup/" + rel[len("backups/"):]
	}
	return sessionID
}

func (ls *ledgerStore) processLogFile(path string, info os.FileInfo, globalDir string, pricing domain_pricing.PricingData) (*sessionCostRecord, error) {
	sessionID := ls.getSessionID(path, globalDir)

	usage, totalCost, detectedModel, timestamp, err := parseUsage(path, pricing, ls.model)
	if err != nil {
		return nil, err
	}

	modelToUse := detectedModel
	if modelToUse == "" {
		modelToUse = ls.model
	}

	// Extract date from path or mod time
	date := info.ModTime().Format("2006-01-02")
	rel, _ := filepath.Rel(globalDir, path)
	rel = filepath.ToSlash(rel)
	if matches := dateRegex.FindStringSubmatch(rel); len(matches) > 3 {
		date = fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
	}

	if timestamp.IsZero() {
		// Fallback to mod time if no timestamp found in logs
		timestamp = info.ModTime()
	}

	return &sessionCostRecord{
		Date:      date,
		Timestamp: timestamp,
		Session:   sessionID,
		Model:     modelToUse,
		TotalCost: totalCost,
		Usage:     usage,
	}, nil
}

func (ls *ledgerStore) persistMergedLedger(ctx context.Context, historyPath string, newRecords []sessionCostRecord) {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	f, err := ls.acquireLedgerLock(historyPath)
	if err != nil {
		return
	}
	defer ls.releaseLedgerLock(historyPath, f)

	history := ls.readExistingRecords(historyPath)
	merged := ls.mergeRecords(history, newRecords)

	if bytes, err := json.Marshal(merged); err == nil {
		if err := persistence.AtomicWrite(ctx, historyPath, bytes, 0644); err != nil {
			log.Printf("Warning: Failed to write ledger %s: %v", historyPath, err)
		}
	}
}

func (ls *ledgerStore) acquireLedgerLock(historyPath string) (*os.File, error) {
	lockPath := historyPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
	if err != nil && os.IsExist(err) {
		if isStale(lockPath) {
			if err := os.Remove(lockPath); err != nil {
				log.Printf("Warning: Failed to remove stale lock %s: %v", lockPath, err)
			}
			f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
		}
	}
	return f, err
}

func (ls *ledgerStore) releaseLedgerLock(historyPath string, f *os.File) {
	lockPath := historyPath + ".lock"
	if f != nil {
		if err := f.Close(); err != nil {
			log.Printf("Warning: Failed to close lock file %s: %v", lockPath, err)
		}
	}
	if err := os.Remove(lockPath); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: Failed to remove lock file %s: %v", lockPath, err)
		}
	}
}

func (ls *ledgerStore) readExistingRecords(historyPath string) []sessionCostRecord {
	var history []sessionCostRecord
	content, err := os.ReadFile(historyPath)
	if err != nil {
		return history
	}
	if err := json.Unmarshal(content, &history); err != nil {
		log.Printf("Warning: Failed to parse ledger %s: %v", historyPath, err)
	}
	return history
}

func (ls *ledgerStore) mergeRecords(history []sessionCostRecord, newRecords []sessionCostRecord) []sessionCostRecord {
	mergedMap := make(map[string]sessionCostRecord)
	for _, r := range history {
		mergedMap[r.Session] = r
	}
	for _, r := range newRecords {
		mergedMap[r.Session] = r
	}

	merged := make([]sessionCostRecord, 0, len(mergedMap))
	for _, r := range mergedMap {
		merged = append(merged, r)
	}
	return merged
}
