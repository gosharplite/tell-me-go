// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

// DriftReport describes configuration mismatches between the canonical
// intended config (a.config) and the live state of the engine and
// context manager.
//
// A report with InSync == true and all sub-drifts nil means all
// components match their intended configuration. The zero value
// (DriftReport{}) correctly represents "no drift detected."
type DriftReport struct {
	// InSync is true when all components match the canonical config.
	InSync bool

	// EngineDrift describes any mismatch in the orchestrator engine.
	// nil means the engine is in sync (or not initialized).
	EngineDrift *EngineDrift

	// CtxManagerDrift describes any mismatch in the context manager.
	// nil means the context manager is in sync (or not initialized).
	CtxManagerDrift *CtxManagerDrift
}

// EngineDrift describes a mismatch between the intended runtime
// configuration and the engine's live state.
type EngineDrift struct {
	ExpectedProvider string
	ActualProvider   string
	ExpectedModel    string
	ActualModel      string
	ExpectedMode     string
	ActualMode       string
}

// CtxManagerDrift describes a mismatch between the intended limits
// and the context manager's live state. Field names match
// events.Limits for a direct 1:1 mapping to the data source.
type CtxManagerDrift struct {
	ExpectedMaxHistoryTokens int
	ActualMaxHistoryTokens   int
	ExpectedMaxToolTurns     int
	ActualMaxToolTurns       int
	ExpectedMaxHistoryTurns  int
	ActualMaxHistoryTurns    int
}

// DiffConfig compares the canonical intended configuration (a.config)
// against the live state of the engine and context manager. It returns
// a DriftReport describing any mismatches.
//
// A report with InSync == true and all sub-drifts nil means all
// components match. The caller should check report.InSync rather than
// testing for a nil report.
//
// The returned report is a point-in-time snapshot: between reading
// a.config and reading live engine/ctx-manager state, a concurrent
// Reconfigure may change the underlying values. This is acceptable
// because DiffConfig is a diagnostic tool, not called in the hot path.
//
// Nil engine and nil ctxManager (e.g., bare agent before full
// initialization) are treated as "no drift" for their respective
// components.
func (a *agent) DiffConfig() *DriftReport {
	cfg := a.config.Load()
	if cfg == nil {
		return &DriftReport{InSync: true}
	}

	report := &DriftReport{InSync: true}

	// Compare engine state
	if a.engine != nil {
		engineSnap := a.engine.GetConfig()
		if cfg.ProviderName != engineSnap.ProviderName ||
			cfg.Model != engineSnap.Model ||
			cfg.Mode != engineSnap.Mode {
			report.InSync = false
			report.EngineDrift = &EngineDrift{
				ExpectedProvider: cfg.ProviderName,
				ActualProvider:   engineSnap.ProviderName,
				ExpectedModel:    cfg.Model,
				ActualModel:      engineSnap.Model,
				ExpectedMode:     cfg.Mode,
				ActualMode:       engineSnap.Mode,
			}
		}
	}

	// Compare context manager state
	if a.ctxManager != nil {
		limits := a.ctxManager.GetLimits()
		if cfg.Limits.MaxHistoryTokens != limits.MaxHistoryTokens ||
			cfg.Limits.MaxToolTurns != limits.MaxToolTurns ||
			cfg.Limits.MaxHistoryTurns != limits.MaxHistoryTurns {
			report.InSync = false
			report.CtxManagerDrift = &CtxManagerDrift{
				ExpectedMaxHistoryTokens: cfg.Limits.MaxHistoryTokens,
				ActualMaxHistoryTokens:   limits.MaxHistoryTokens,
				ExpectedMaxToolTurns:     cfg.Limits.MaxToolTurns,
				ActualMaxToolTurns:       limits.MaxToolTurns,
				ExpectedMaxHistoryTurns:  cfg.Limits.MaxHistoryTurns,
				ActualMaxHistoryTurns:    limits.MaxHistoryTurns,
			}
		}
	}

	return report
}
