// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/ui"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	mediasvc "github.com/gosharplite/tell-me-go/internal/services/media"
	"github.com/gosharplite/tell-me-go/internal/tools/code"
	"github.com/gosharplite/tell-me-go/internal/tools/dev"
	"github.com/gosharplite/tell-me-go/internal/tools/files"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/git"
	"github.com/gosharplite/tell-me-go/internal/tools/media"
	"github.com/gosharplite/tell-me-go/internal/tools/network"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/system"
)

func (a *App) initPaths(cfg *config.Config) (*sessionPaths, error) {
	modeDir := filepath.Join(a.homeDir, "output", cfg.Mode)
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory [%s]: %v", modeDir, err)
	}

	return &sessionPaths{
		modeDir:              modeDir,
		historyPath:          filepath.Join(modeDir, "history.json"),
		logPath:              filepath.Join(modeDir, "tokens.log"),
		commandsLogPath:      filepath.Join(modeDir, "commands.log"),
		safePathsPath:        filepath.Join(modeDir, "safepaths.json"),
		readPathsPath:        filepath.Join(modeDir, "readpaths.json"),
		bypassPath:           filepath.Join(modeDir, "bypass.log"),
		persistentConfigPath: filepath.Join(modeDir, "config.json"),
	}, nil
}

func (a *App) loadPersistentConfig(paths *sessionPaths, cfg *config.Config) (map[string]string, error) {
	pCfg := make(map[string]string)
	if data, err := os.ReadFile(paths.persistentConfigPath); err == nil {
		_ = json.Unmarshal(data, &pCfg)
	}

	updated := false
	seedLimit := func(key string, val int) {
		if _, ok := pCfg[key]; !ok {
			pCfg[key] = fmt.Sprintf("%d", val)
			updated = true
		}
	}
	seedLimit("MAX_HISTORY_TOKENS", cfg.MaxHistoryTokens)
	seedLimit("MAX_TOOL_TURNS", cfg.MaxToolTurns)
	seedLimit("MAX_HISTORY_TURNS", cfg.MaxHistoryTurns)

	if updated {
		if data, err := json.MarshalIndent(pCfg, "", "  "); err == nil {
			_ = os.WriteFile(paths.persistentConfigPath, data, 0644)
		}
	}
	return pCfg, nil
}

func (a *App) setupSecurity(paths *sessionPaths, opts *cliOptions, cfg *config.Config) {
	a.sm.SetSafePathsFile(paths.safePathsPath)
	a.sm.SetReadOnlyPathsFile(paths.readPathsPath)
	a.sm.SetBypassFile(paths.bypassPath)
	a.sm.SetCommandsLogFile(paths.commandsLogPath)

	if err := a.sm.LoadSafePaths(); err != nil {
		log.Printf("Warning: Failed to load persistent safe paths: %v", err)
	}
	if err := a.sm.LoadReadOnlyPaths(); err != nil {
		log.Printf("Warning: Failed to load persistent read-only paths: %v", err)
	}
	a.sm.LoadBypassState()

	a.sm.RegisterSafePath(filepath.Join(a.homeDir, "output"))
	a.sm.RegisterReadOnlyPath(opts.configPath)
}

func (a *App) handleNewSession(paths *sessionPaths, cfg *config.Config, pricingOverrides map[string]llm.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	// Record cost with a unique ID including the timestamp before archiving
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.logPath))
	_ = framework.RecordSessionCost(context.Background(), a.sm, nil, paths.logPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides)
	a.archiveSessionFilesWithTimestamp(a.homeDir, timestamp, paths.historyPath, paths.logPath, paths.commandsLogPath)
	a.cleanupOldBackups(a.homeDir, cfg.Mode)
}

func (a *App) setupRegistry(client *api.Client, cfg *config.Config, paths *sessionPaths, pricingOverrides map[string]llm.ModelPricing) *registry.Registry {
	reg := registry.New()

	files.Register(reg, a.sm)
	code.Register(reg, a.sm)
	system.Register(reg, a.sm)
	git.Register(reg, a.sm)
	dev.Register(reg, a.sm)
	network.Register(reg, a.sm)
	framework.RegisterState(reg, a.sm, paths.modeDir)
	framework.RegisterPolicy(reg, a.sm)
	framework.RegisterMetrics(reg, a.sm, paths.logPath, cfg.Model, cfg.Mode, pricingOverrides)
	dev.RegisterRelease(reg, a.sm)
	media.Register(reg, a.sm, mediasvc.NewService(client, filepath.Join(a.homeDir, "assets/generated")))

	return reg
}

func (a *App) applyConfiguration(chatAgent agent.Chatter, cfg *config.Config, opts *cliOptions, paths *sessionPaths, pruned int, pricing llm.PricingData) {
	chatAgent.SetPersistentConfigPath(paths.persistentConfigPath)
	chatAgent.SetMainConfigPath(opts.configPath)
	chatAgent.SetLogFile(paths.logPath)

	// Create and subscribe UI sidecar
	renderer := ui.NewStdUIRenderer(a.sm)
	subscriber := NewUISubscriber(renderer, cfg.ShowThoughts, cfg.ShowTools, opts.rawOutput, paths.logPath)
	chatAgent.Subscribe(subscriber.HandleEvent)

	// Resolve model-specific limits
	maxTokens := cfg.MaxHistoryTokens
	if mCfg, ok := cfg.Models[cfg.Model]; ok && mCfg.ContextWindow > 0 {
		if maxTokens > mCfg.ContextWindow {
			maxTokens = mCfg.ContextWindow
		}
	} else {
		for k, v := range cfg.Models {
			if k != "default" && strings.Contains(cfg.Model, k) && v.ContextWindow > 0 {
				if maxTokens > v.ContextWindow {
					maxTokens = v.ContextWindow
				}
				break
			}
		}
	}

	// Resolve tiered threshold from pricing
	threshold := config.DefaultTieredThreshold
	if mPricing, ok := pricing.Models[cfg.Model]; ok && mPricing.TieredThreshold > 0 {
		threshold = int(mPricing.TieredThreshold)
	} else {
		// Fallback to substring match
		for k, v := range pricing.Models {
			if k != "default" && strings.Contains(cfg.Model, k) && v.TieredThreshold > 0 {
				threshold = int(v.TieredThreshold)
				break
			}
		}
	}

	chatAgent.SetLimits(cfg.MaxToolTurns, maxTokens, cfg.MaxHistoryTurns)
	chatAgent.SetTieredThreshold(threshold)
	chatAgent.SetPrunedTurns(pruned)
	chatAgent.SetConcurrency(cfg.MaxConcurrentTools, cfg.ToolTimeoutSeconds)
}

func (a *App) archiveSessionFilesWithTimestamp(homeDir, timestamp string, filesToMove ...string) {
	backupDir := filepath.Join(homeDir, "output", "backups", timestamp)

	backupCreated := false
	for _, f := range filesToMove {
		if _, err := os.Stat(f); err == nil {
			if !backupCreated {
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					func() {
						a.sm.TerminalLock()
						defer a.sm.TerminalUnlock()
						fmt.Fprintf(a.Stderr, "Error creating backup directory: %v\n", err)
					}()
					return
				}
				func() {
					a.sm.TerminalLock()
					defer a.sm.TerminalUnlock()
					fmt.Fprintf(a.Stdout, "Archiving existing session files to %s\n", backupDir)
				}()
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := os.Rename(f, dest); err != nil {
				func() {
					a.sm.TerminalLock()
					defer a.sm.TerminalUnlock()
					fmt.Fprintf(a.Stderr, "Error archiving %s: %v\n", f, err)
				}()
			}
		}
	}
}

func (a *App) cleanupOldBackups(homeDir, mode string) {
	backupBaseDir := filepath.Join(homeDir, "output", "backups")
	entries, err := os.ReadDir(backupBaseDir)
	if err != nil {
		return // Likely doesn't exist yet
	}

	retentionDays := 30
	configPath := filepath.Join(homeDir, "output", mode, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg map[string]string
		if err := json.Unmarshal(data, &cfg); err == nil {
			if val, ok := cfg["backup_retention_days"]; ok {
				if days, err := strconv.Atoi(val); err == nil {
					retentionDays = days
				}
			}
		}
	}

	if retentionDays <= 0 {
		return // 0 or negative means keep forever
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Format: YYYYMMDD_HHMMSS (15 chars)
		if len(entry.Name()) < 15 {
			continue
		}

		folderTime, err := time.Parse("20060102_150405", entry.Name()[:15])
		if err != nil {
			continue
		}

		if folderTime.Before(cutoff) {
			path := filepath.Join(backupBaseDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				func() {
					a.sm.TerminalLock()
					defer a.sm.TerminalUnlock()
					fmt.Fprintf(a.Stderr, "Warning: Failed to cleanup old backup %s: %v\n", path, err)
				}()
			}
		}
	}
}
