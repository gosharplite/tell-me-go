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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// isProcessAlive checks whether the given PID corresponds to a running process.
// On Unix, it uses syscall.Signal(0) to probe liveness.
// On Windows, it always returns true, letting the time-based fallback in isStale
// handle stale lock detection.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// On Windows, syscall.Signal(0) is not available.
	// Fall back to time-based staleness in isStale.
	if runtime.GOOS == "windows" {
		return true
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}

	// ESRCH: no such process — definitely dead.
	if errors.Is(err, syscall.ESRCH) {
		return false
	}

	// EPERM or os.ErrPermission: process exists but we don't own it — alive.
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		return true
	}

	// Any other error: assume dead.
	return false
}

// isStale checks if a lock file is stale and safe to break.
// It first reads the PID from the lock file and checks process liveness.
// If the PID-based check is inconclusive, it falls back to a 10-second
// modification-time threshold. Legitimate lock holds take < 10ms.
func isStale(path string) bool {
	// Attempt PID-based liveness check.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true // lock is gone, free to acquire
		}
		// Can't read — fall through to stat check below.
	} else {
		pidStr := strings.TrimSpace(string(data))
		if pidStr != "" {
			if pid, parseErr := strconv.Atoi(pidStr); parseErr == nil && pid > 0 {
				if !isProcessAlive(pid) {
					return true // owning process is dead, lock is stale
				}
				return false // owning process is alive, lock is valid
			}
		}
	}

	// Fallback: time-based staleness check (10 seconds).
	if info, statErr := os.Stat(path); statErr == nil {
		return time.Since(info.ModTime()) > 10*time.Second
	}
	return false
}

// acquireLedgerLock attempts to create an exclusive lock file with stale-lock recovery
// and exponential backoff retry. On success, it writes the current PID into the lock
// file so that other processes can determine liveness.
//
// The function retries up to 5 times with exponential backoff (50ms base) when the
// lock is held by a live process. For stale locks (dead PID or old timestamp), it
// removes the lock and continues to the next iteration, where the atomic O_EXCL
// open prevents TOCTOU races.
func acquireLedgerLock(lockPath string) (*os.File, error) {
	const maxRetries = 5
	delay := 50 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			// Write PID to the lock file for liveness detection.
			pid := os.Getpid()
			if _, writeErr := fmt.Fprintf(f, "%d", pid); writeErr != nil {
				closeErr := f.Close()
				removeErr := os.Remove(lockPath)
				if closeErr != nil {
					slog.Warn("failed to close lock file after PID write failure", slog.String("lock_path", lockPath), slog.Any("error", closeErr))
				}
				if removeErr != nil {
					slog.Warn("failed to remove lock file after PID write failure", slog.String("lock_path", lockPath), slog.Any("error", removeErr))
				}
				return nil, fmt.Errorf("write PID to lock file %s: %w", lockPath, writeErr)
			}
			if syncErr := f.Sync(); syncErr != nil {
				closeErr := f.Close()
				removeErr := os.Remove(lockPath)
				if closeErr != nil {
					slog.Warn("failed to close lock file after sync failure", slog.String("lock_path", lockPath), slog.Any("error", closeErr))
				}
				if removeErr != nil {
					slog.Warn("failed to remove lock file after sync failure", slog.String("lock_path", lockPath), slog.Any("error", removeErr))
				}
				return nil, fmt.Errorf("sync lock file %s: %w", lockPath, syncErr)
			}
			return f, nil
		}

		// Non-IsExist errors are fatal (e.g., permission denied).
		if !os.IsExist(err) {
			return nil, err
		}

		// Lock exists. Check if it's stale.
		if isStale(lockPath) {
			if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
				slog.Warn("failed to remove stale lock", slog.String("lock_path", lockPath), slog.Any("error", removeErr))
			}
			// Do NOT try to open again here — continue to the next loop iteration.
			// The atomic O_EXCL at the top of the loop prevents TOCTOU races.
			continue
		}

		// Lock exists and is held by a live process. Back off and retry.
		time.Sleep(delay)
		delay *= 2
	}

	return nil, fmt.Errorf("failed to acquire ledger lock after %d retries: file exists", maxRetries)
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
