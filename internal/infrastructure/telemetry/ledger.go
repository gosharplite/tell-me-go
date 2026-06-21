// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// filepathRel is the filepath.Rel function, overridable in tests.
var filepathRel = filepath.Rel

const ledgerRecoveryTimeout = 10 * time.Minute

// ledgerStore handles persistence and recovery of cost metrics.
type ledgerStore struct {
	sm               domain_security.Manager
	model            string
	pricingOverrides map[string]domain_pricing.ModelPricing
	getSessionIDFunc func(path, globalDir string) (string, error) // test-only override; when set, getSessionID delegates to this
}

// newLedgerStore creates a new ledgerStore.
func newLedgerStore(sm domain_security.Manager, model string, pricingOverrides map[string]domain_pricing.ModelPricing) *ledgerStore {
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

// acquireLedgerLock attempts to create an exclusive lock file with stale-lock recovery.
func acquireLedgerLock(lockPath string) (*os.File, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
	if err != nil && os.IsExist(err) {
		if isStale(lockPath) {
			if err := os.Remove(lockPath); err != nil {
				slog.Warn("failed to remove stale lock", slog.String("lock_path", lockPath), slog.Any("error", err))
			}
			f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
		}
	}
	return f, err
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
		slog.Warn("recovery walk errors", slog.Any("error", err))
		if len(files) == 0 {
			return // nothing to recover
		}
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
		slog.Warn("failed to parse existing ledger during recovery", slog.Any("error", err))
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

// tryProcessLogFile attempts to process a single log file into a
// sessionCostRecord. It returns nil when the file has already been
// seen, is inaccessible, or cannot be parsed. Non-fatal errors are
// logged and result in nil.
func (ls *ledgerStore) tryProcessLogFile(ctx context.Context, path string, globalDir string, seen map[string]bool, pricing domain_pricing.PricingData) *sessionCostRecord {
	sessionID, err := ls.getSessionID(path, globalDir)
	if err != nil {
		slog.Debug("recovery skipping file", slog.String("path", path), slog.Any("error", err))
		return nil
	}
	if seen[sessionID] {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	record, err := ls.processLogFile(path, info, globalDir, pricing)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("recovery failed to parse file", slog.String("path", path), slog.Any("error", err))
		}
		return nil
	}

	return record
}

// discoverNewRecords scans the list of log files for new sessions not yet in the ledger.
func (ls *ledgerStore) discoverNewRecords(ctx context.Context, files []string, globalDir string, seen map[string]bool, pricing domain_pricing.PricingData) []sessionCostRecord {
	var discovered []sessionCostRecord
	for _, path := range files {
		if ctx.Err() != nil {
			break
		}

		record := ls.tryProcessLogFile(ctx, path, globalDir, seen, pricing)
		if record != nil {
			discovered = append(discovered, *record)
			seen[record.Session] = true
		}
	}
	return discovered
}

func (ls *ledgerStore) findLogFiles(globalDir string) ([]string, error) {
	var files []string
	var walkErrors []error
	err := filepath.WalkDir(globalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			walkErrors = append(walkErrors, fmt.Errorf("access %s: %w", path, err))
			return nil // continue walking; don't abort the entire recovery
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), "tokens.log") {
			files = append(files, path)
		}
		return nil
	})
	if len(walkErrors) > 0 {
		return files, fmt.Errorf("walk errors during recovery (%d): %w", len(walkErrors), errors.Join(walkErrors...))
	}
	return files, err
}

func (ls *ledgerStore) getSessionID(path, globalDir string) (string, error) {
	if ls.getSessionIDFunc != nil {
		return ls.getSessionIDFunc(path, globalDir)
	}
	rel, err := filepathRel(globalDir, path)
	if err != nil {
		return "", fmt.Errorf("resolving session ID for %s relative to %s: %w", path, globalDir, err)
	}
	rel = filepath.ToSlash(rel)
	sessionID := rel
	if strings.HasPrefix(rel, "backups/") {
		sessionID = "backup/" + rel[len("backups/"):]
	}
	return sessionID, nil
}

func (ls *ledgerStore) processLogFile(path string, info os.FileInfo, globalDir string, pricing domain_pricing.PricingData) (*sessionCostRecord, error) {
	sessionID, err := ls.getSessionID(path, globalDir)
	if err != nil {
		return nil, fmt.Errorf("processing log file %s: %w", path, err)
	}

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

	f, err := acquireLedgerLock(historyPath + ".lock")
	if err != nil {
		slog.Warn("failed to acquire ledger lock", slog.String("lock_path", historyPath+".lock"), slog.String("reason", "contention"), slog.Any("error", err))
		return
	}
	defer ls.releaseLedgerLock(historyPath, f)

	history := ls.readExistingRecords(historyPath)
	merged := ls.mergeRecords(history, newRecords)

	if bytes, err := json.Marshal(merged); err == nil {
		if err := persistence.AtomicWrite(ctx, &persistence.OSFileSystem{}, historyPath, bytes, 0644); err != nil {
			slog.Warn("failed to write ledger", slog.String("path", historyPath), slog.Any("error", err))
		}
	}
}

func (ls *ledgerStore) releaseLedgerLock(historyPath string, f *os.File) {
	lockPath := historyPath + ".lock"
	if f != nil {
		if err := f.Close(); err != nil {
			slog.Warn("failed to close lock file", slog.String("lock_path", lockPath), slog.Any("error", err))
		}
	}
	if err := os.Remove(lockPath); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to remove lock file", slog.String("lock_path", lockPath), slog.Any("error", err))
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
		slog.Warn("failed to parse ledger", slog.String("path", historyPath), slog.Any("error", err))
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
