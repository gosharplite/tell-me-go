// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type metricsManager struct {
	sm               domain_security.Manager
	metricsMu        sync.Mutex
	logFile          string
	traceFile        string
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	ledger           *ledgerStore
	kvStore          ports.KVStore
}

type costSummaryArgs struct {
	Billing   bool   `json:"billing"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Interval  string `json:"interval"` // "hour" or "day"
	GroupBy   string `json:"group_by"` // NEW: "date" (default), "model", or "date,model"
}

type estimateCostArgs struct{}

// fileSystem is the filesystem abstraction, overridable in tests.
var fileSystem FileSystem = osFS{}

// resolveUsageForSummaryFunc is the resolveUsageForSummary function, overridable in tests.
var resolveUsageForSummaryFunc = resolveUsageForSummary

// TraceLogger encapsulates dependencies for writing TurnTrace events to disk.
// All fields are injected via the constructor and can be overridden in tests
// without mutating package-level state, enabling safe test parallelization.
type TraceLogger struct {
	marshalFunc   func(v any) ([]byte, error)
	openTraceFile func(path string) (io.WriteCloser, error)
	logger        *slog.Logger
}

// NewTraceLogger creates a TraceLogger with production defaults.
// marshalFunc is set to json.Marshal and openTraceFile to os.OpenFile.
// If logger is nil, slog.Default() is used.
func NewTraceLogger(logger *slog.Logger) *TraceLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &TraceLogger{
		marshalFunc: json.Marshal,
		openTraceFile: func(path string) (io.WriteCloser, error) {
			return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		},
		logger: logger,
	}
}

// RegisterMetrics adds tools for usage and cost analysis to the registry.
func RegisterMetrics(r tools.Registry, sm domain_security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]domain_pricing.ModelPricing, kvStore ports.KVStore) error {
	m := &metricsManager{
		sm:               sm,
		logFile:          logFile,
		traceFile:        traceFile,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
		ledger:           newLedgerStore(sm, model, pricingOverrides),
		kvStore:          kvStore,
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "estimate_cost",
		Description: "Calculates the estimated USD cost of the current session.",
	}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var eArgs estimateCostArgs
		if err := tools.UnmarshalArgs(args, &eArgs); err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
		res, err := m.EstimateCost(ctx, true, "") // Records to ledger with default ID
		return tools.ToolResult{Text: res}, err
	}, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "get_cost_summary",
		Description: "Returns a summary of total AI costs grouped by date from the local history ledger.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"billing": {
					Type:        "BOOLEAN",
					Description: "If true, aggregates costs using Google Billing timezone (UTC-8).",
				},
				"start_date": {
					Type:        "STRING",
					Description: "The start date for the summary (YYYY-MM-DD).",
				},
				"end_date": {
					Type:        "STRING",
					Description: "The end date for the summary (YYYY-MM-DD).",
				},
				"interval": {
					Type:        "STRING",
					Description: "Aggregation interval: 'hour' or 'day' (default: 'day').",
				},
				"group_by": {
					Type:        "STRING",
					Description: "NEW: 'date' (default), 'model', or 'date,model'.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var sArgs costSummaryArgs
		if err := tools.UnmarshalArgs(args, &sArgs); err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}

		m.recordCostSilently(ctx)
		res, err := m.getCostSummary(ctx, sArgs)
		return tools.ToolResult{Text: res}, err
	}, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}
	return nil
}

// RecordSessionCost calculates and saves the session cost to the global ledger and appends a summary to the log.
func RecordSessionCost(ctx context.Context, sm domain_security.Manager, tracker domain_pricing.CostTracker, logPath, model, mode, sessionID string, pricingOverrides map[string]domain_pricing.ModelPricing) error {
	m := &metricsManager{
		sm:               sm,
		logFile:          logPath,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
		ledger:           newLedgerStore(sm, model, pricingOverrides),
	}

	// 1. Record to global ledger (detailed breakdown)
	_, err := m.EstimateCost(ctx, true, sessionID)
	if err != nil {
		return fmt.Errorf("failed to estimate and record session cost: %w", err)
	}

	// 2. Resolve usage stats
	usage, totalCost, err := resolveUsageForSummaryFunc(ctx, sm, tracker, logPath, model, pricingOverrides)
	if err != nil {
		return err
	}

	// 3. Append summary to log
	return appendSummaryToLog(logPath, usage, totalCost, model)
}

func resolveUsageForSummary(ctx context.Context, sm domain_security.Manager, tracker domain_pricing.CostTracker, logPath, model string, overrides map[string]domain_pricing.ModelPricing) (domain_pricing.UsageStats, float64, error) {
	if tracker != nil {
		usage, totalCost := tracker.GetStats(ctx)
		return usage, totalCost, nil
	}

	pd := GetPricing(ctx, sm, filepath.Dir(logPath))
	for k, v := range overrides {
		pd.Models[k] = v
	}

	usage, totalCost, _, _, err := parseUsage(logPath, pd, model)
	if err != nil {
		if os.IsNotExist(err) {
			return domain_pricing.UsageStats{}, 0, nil
		}
		return domain_pricing.UsageStats{}, 0, fmt.Errorf("failed to parse usage log for summary: %w", err)
	}
	return usage, totalCost, nil
}

// openLogFileForAppend opens a log file for appending, creating parent directories
// if they don't exist.
func openLogFileForAppend(logPath string) (File, error) {
	f, err := fileSystem.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		// Ensure directory exists if we are meant to create it
		if os.IsNotExist(err) {
			if mkdirErr := fileSystem.MkdirAll(filepath.Dir(logPath), 0755); mkdirErr != nil {
				return nil, fmt.Errorf("failed to open log file %q for summary append (also failed to create dir: %v): %w", logPath, mkdirErr, err)
			}
			f, err = fileSystem.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				return nil, fmt.Errorf("failed to open log file %q for summary append after mkdir: %w", logPath, err)
			}
		} else {
			return nil, fmt.Errorf("failed to open log file %q for summary append: %w", logPath, err)
		}
	}
	return f, nil
}

func appendSummaryToLog(logPath string, usage domain_pricing.UsageStats, totalCost float64, model string) error {
	if usage.PromptTokens == 0 && usage.ResponseTokens == 0 && usage.SearchQueries == 0 {
		return nil
	}

	summary := llm.Metrics{
		Timestamp:      time.Now().Format(time.RFC3339),
		Model:          model,
		CachedTokens:   int32(usage.CachedTokens),
		PromptTokens:   int32(usage.PromptTokens),
		ResponseTokens: int32(usage.ResponseTokens),
		TotalTokens:    int32(usage.PromptTokens + usage.ResponseTokens),
		SearchQueries:  int(usage.SearchQueries),
		Cost:           totalCost,
		IsSummary:      true,
	}

	summaryBytes, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("failed to marshal cost summary: %w", err)
	}

	fAppend, err := openLogFileForAppend(logPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = fAppend.Close()
	}()

	_, err = io.WriteString(fAppend, string(summaryBytes)+"\n")
	if err != nil {
		return fmt.Errorf("failed to write cost summary to log: %w", err)
	}
	return nil
}

// recordCostSilently calls EstimateCost and logs a warning on failure
// without interrupting the caller. Extracted for testability.
func (m *metricsManager) recordCostSilently(ctx context.Context) {
	if _, err := m.EstimateCost(ctx, true, ""); err != nil {
		slog.Warn("failed to record cost before summary",
			slog.Any("error", err))
	}
}

// generateSessionID creates a unique identifier for a session based on its mode and log file name.
// This ID is used as the unique key in global_costs.json to identify and update session records.
func generateSessionID(mode, logFile string) string {
	return filepath.ToSlash(filepath.Join(mode, filepath.Base(logFile)))
}

// logTrace writes a TurnTrace to a trace log file.
func (t *TraceLogger) logTrace(ctx context.Context, traceFile string, trace *domain_telemetry.TurnTrace) {
	if traceFile == "" || trace == nil {
		return
	}

	// 1. Immediate context check
	select {
	case <-ctx.Done():
		return
	default:
	}

	data, err := t.marshalFunc(trace)
	if err != nil {
		t.logger.Warn("failed to marshal TurnTrace",
			slog.Any("error", err))
		return
	}

	t.writeTraceEntry(traceFile, data)
}

// writeTraceEntry opens the trace file, writes a JSON line, and closes it.
// All errors are logged as warnings; this is a fire-and-forget operation
// called from the event subscriber pipeline.
func (t *TraceLogger) writeTraceEntry(traceFile string, data []byte) {
	wc, err := t.openTraceFile(traceFile)
	if err != nil {
		t.logger.Warn("failed to open trace file",
			slog.String("path", traceFile),
			slog.Any("error", err))
		return
	}
	defer func() {
		if cerr := wc.Close(); cerr != nil {
			t.logger.Warn("failed to close trace file",
				slog.String("path", traceFile),
				slog.Any("error", cerr))
		}
	}()

	if _, err := wc.Write(append(data, '\n')); err != nil {
		t.logger.Warn("failed to write to trace file",
			slog.String("path", traceFile),
			slog.Any("error", err))
	}
}

// RegisterTraceSubscriber subscribes a listener to TraceEvents.
func RegisterTraceSubscriber(bus events.EventBus, traceFile string) {
	tl := NewTraceLogger(slog.Default())
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if te, ok := e.(events.TraceEvent); ok {
			tl.logTrace(ctx, traceFile, te.Trace)
		}
	})
}
