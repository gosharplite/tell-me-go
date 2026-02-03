// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

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

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/security"
)

// SessionCostRecord represents a single session's financial footprint.
type SessionCostRecord struct {
	Date      string             `json:"date"`
	Session   string             `json:"session"`
	Model     string             `json:"model"`
	TotalCost float64            `json:"total_cost"`
	Usage     pricing.UsageStats `json:"usage,omitempty"`
}

var (
	recoveryInProgress sync.Map // historyPath -> bool
	dateRegex          = regexp.MustCompile(`(\d{4})[-_/]?(\d{2})[-_/]?(\d{2})`)
	ledgerMu           sync.Mutex
)

const ledgerRecoveryTimeout = 10 * time.Minute

// LedgerStore handles persistence and recovery of cost metrics.
type LedgerStore struct {
	sm               *security.SecurityManager
	model            string
	pricingOverrides map[string]pricing.ModelPricing
}

// NewLedgerStore creates a new LedgerStore.
func NewLedgerStore(sm *security.SecurityManager, model string, pricingOverrides map[string]pricing.ModelPricing) *LedgerStore {
	return &LedgerStore{
		sm:               sm,
		model:            model,
		pricingOverrides: pricingOverrides,
	}
}

// breakStaleLock removes a lock file if it's older than 5 minutes to prevent deadlocks after crashes.
func breakStaleLock(lockPath string) {
	if info, err := os.Stat(lockPath); err == nil {
		if time.Since(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(lockPath)
		}
	}
}

// RecoverLedger crawls backups and mode directories to reconstruct a missing global_costs.json.
func (ls *LedgerStore) RecoverLedger(ctx context.Context, globalDir string) {
	historyPath := filepath.Join(globalDir, "global_costs.json")
	if _, loaded := recoveryInProgress.LoadOrStore(historyPath, true); loaded {
		return
	}
	defer recoveryInProgress.Delete(historyPath)

	var history []SessionCostRecord
	if content, err := os.ReadFile(historyPath); err == nil {
		_ = json.Unmarshal(content, &history)
	}

	seen := make(map[string]bool)
	for _, r := range history {
		seen[r.Session] = true
	}

	// Fetch pricing once for all calculations
	pricing := GetPricing(ctx, ls.sm, globalDir)
	for k, v := range ls.pricingOverrides {
		pricing.Models[k] = v
	}

	// 1. Walk through all subdirectories
	err := filepath.Walk(globalDir, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			log.Printf("Recovery: error accessing path %q: %v\n", path, err)
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), "tokens.log") {
			return nil
		}

		rel, _ := filepath.Rel(globalDir, path)
		rel = filepath.ToSlash(rel)
		sessionID := rel
		if strings.HasPrefix(rel, "backups/") {
			sessionID = "backup/" + rel[len("backups/"):]
		}

		if seen[sessionID] {
			return nil
		}

		usage, totalCost, detectedModel, err := ParseUsage(path, pricing, ls.model)
		if err == nil {
			modelToUse := detectedModel
			if modelToUse == "" {
				modelToUse = ls.model
			}

			// Extract date from path or mod time
			date := info.ModTime().Format("2006-01-02")
			if matches := dateRegex.FindStringSubmatch(rel); len(matches) > 3 {
				date = fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
			}

			history = append(history, SessionCostRecord{
				Date:      date,
				Session:   sessionID,
				Model:     modelToUse,
				TotalCost: totalCost,
				Usage:     usage,
			})
			seen[sessionID] = true
		} else if !os.IsNotExist(err) {
			log.Printf("Recovery: failed to parse %s: %v\n", path, err)
		}
		return nil
	})

	if err != nil {
		log.Printf("Recovery: walk failed: %v\n", err)
	}

	if len(history) > 0 {
		ledgerMu.Lock()
		defer ledgerMu.Unlock()

		lockPath := historyPath + ".lock"
		breakStaleLock(lockPath)
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return // Another process is writing; skip this background update
		}
		defer func() {
			lock.Close()
			os.Remove(lockPath)
		}()

		// Re-read and merge in case of concurrent updates during walk
		if content, err := os.ReadFile(historyPath); err == nil {
			var latest []SessionCostRecord
			if err := json.Unmarshal(content, &latest); err == nil {
				mergedMap := make(map[string]SessionCostRecord)
				for _, r := range history {
					mergedMap[r.Session] = r
				}
				for _, r := range latest {
					mergedMap[r.Session] = r
				}
				history = make([]SessionCostRecord, 0, len(mergedMap))
				for _, r := range mergedMap {
					history = append(history, r)
				}
			}
		}

		if bytes, err := json.Marshal(history); err == nil {
			_ = fsutil.AtomicWrite(ctx, historyPath, bytes, 0644)
		}
	}
}
