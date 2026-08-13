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
	"time"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type metricsManager struct {
	sm               domain_security.Manager
	logFile          string
	traceFile        string
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	fs               FileSystem
}

type estimateCostArgs struct{}

// resolveUsageForSummaryFunc is the resolveUsageForSummary function, overridable in tests.
var resolveUsageForSummaryFunc = resolveUsageForSummary

// TraceLogger encapsulates dependencies for writing TurnTrace events to disk.
// All fields are injected via the constructor and can be overridden in tests
// without mutating package-level state, enabling safe test parallelization.
type traceLogger struct {
	marshalFunc   func(v any) ([]byte, error)
	openTraceFile func(path string) (io.WriteCloser, error)
	logger        *slog.Logger
}

// NewTraceLogger creates a TraceLogger with production defaults.
// marshalFunc is set to json.Marshal and openTraceFile to os.OpenFile.
// If logger is nil, slog.Default() is used.
func newTraceLogger(logger *slog.Logger) *traceLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &traceLogger{
		marshalFunc: json.Marshal,
		openTraceFile: func(path string) (io.WriteCloser, error) {
			return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		},
		logger: logger,
	}
}

// RegisterMetrics adds tools for usage and cost analysis to the registry.
func RegisterMetrics(r tools.Registry, sm domain_security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]domain_pricing.ModelPricing) error {
	m := &metricsManager{
		sm:               sm,
		logFile:          logFile,
		traceFile:        traceFile,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
		fs:               osFS{},
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "estimate_cost",
		Description: "Calculates the estimated USD cost of the current session.",
	}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var eArgs estimateCostArgs
		if err := tools.UnmarshalArgs(args, &eArgs); err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
		res, err := m.EstimateCost(ctx)
		return tools.ToolResult{Text: res}, err
	}, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}
	return nil
}

// RecordSessionCost calculates the session's usage statistics and appends a
// summary line to the tokens log. It is best-effort: it does not fail the
// session when the log is missing or cannot be parsed.
func RecordSessionCost(ctx context.Context, sm domain_security.Manager, tracker domain_pricing.CostTracker, logPath, model, mode string, pricingOverrides map[string]domain_pricing.ModelPricing) error {
	m := &metricsManager{
		sm:               sm,
		logFile:          logPath,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
		fs:               osFS{},
	}

	// 1. Resolve usage stats
	usage, totalCost, err := resolveUsageForSummaryFunc(ctx, sm, tracker, logPath, model, pricingOverrides)
	if err != nil {
		return err
	}

	// 2. Append summary to log
	return m.appendSummaryToLog(logPath, usage, totalCost, model)
}

func resolveUsageForSummary(ctx context.Context, sm domain_security.Manager, tracker domain_pricing.CostTracker, logPath, model string, overrides map[string]domain_pricing.ModelPricing) (domain_pricing.UsageStats, float64, error) {
	if tracker != nil {
		usage, totalCost := tracker.GetStats(ctx)
		return usage, totalCost, nil
	}

	pd := domain_config.DefaultPricing()
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
func (m *metricsManager) openLogFileForAppend(logPath string) (File, error) {
	f, err := m.fs.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		// Ensure directory exists if we are meant to create it
		if os.IsNotExist(err) {
			if mkdirErr := m.fs.MkdirAll(filepath.Dir(logPath), 0755); mkdirErr != nil {
				return nil, fmt.Errorf("failed to open log file %q for summary append (also failed to create dir: %v): %w", logPath, mkdirErr, err)
			}
			f, err = m.fs.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				return nil, fmt.Errorf("failed to open log file %q for summary append after mkdir: %w", logPath, err)
			}
		} else {
			return nil, fmt.Errorf("failed to open log file %q for summary append: %w", logPath, err)
		}
	}
	return f, nil
}

func (m *metricsManager) appendSummaryToLog(logPath string, usage domain_pricing.UsageStats, totalCost float64, model string) error {
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

	fAppend, err := m.openLogFileForAppend(logPath)
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

// logTrace writes a TurnTrace to a trace log file.
func (t *traceLogger) logTrace(ctx context.Context, traceFile string, trace *domain_telemetry.TurnTrace) {
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
func (t *traceLogger) writeTraceEntry(traceFile string, data []byte) {
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
	tl := newTraceLogger(slog.Default())
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if te, ok := e.(events.TraceEvent); ok {
			tl.logTrace(ctx, traceFile, te.Trace)
		}
	})
}
