// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/term"
)

// stdUIRenderer implements ports.UIRenderer using standard output/error and Glamour.
type stdUIRenderer struct {
	locker          domain_security.Manager
	stdout          io.Writer
	stderr          io.Writer
	clock           clock.Clock
	renderer        *glamour.TermRenderer
	mu              sync.RWMutex
	ioMu            sync.Mutex
	useColor        bool
	forceSpinner    bool
	metricsProvider ports.SystemMetricsProvider
	lastCPUTime     int64
	lastIdleTime    int64
	lastSampleTime  time.Time
	lastCPUPercent  float64
	lastMemPercent  float64
	markdownErrOnce sync.Once
}

type defaultMetricsProvider struct{}

func (d *defaultMetricsProvider) GetCPUStats() (int64, int64) { return 0, 0 }
func (d *defaultMetricsProvider) GetMemoryPercent() float64   { return 0.0 }

// healthStatusRenderer handles status-to-color mapping and overall status formatting
type healthStatusRenderer struct{}

// renderOverallStatus renders the header and overall status section
func (hsr *healthStatusRenderer) renderOverallStatus(ui uiState, stderr io.Writer, report *ports.HealthReport) {
	overallColor := colorGreen
	switch report.OverallStatus {
	case ports.StatusUnhealthy:
		overallColor = colorRed
	case ports.StatusDegraded:
		overallColor = colorYellow
	}

	writeBestEffort(stderr, "Overall Status: %s%s%s\n", ui.c(overallColor), strings.ToUpper(string(report.OverallStatus)), ui.c(colorReset))
	writeBestEffort(stderr, "Timestamp:      %s\n\n", report.Timestamp.Format(time.RFC3339))
}

// componentDetailRenderer handles component message and detail rendering
type componentDetailRenderer struct{}

// renderComponents iterates through all components and renders each one
func (cdr *componentDetailRenderer) renderComponents(ui uiState, stderr io.Writer, report *ports.HealthReport) {
	components := []ports.Component{ports.CompPersistence, ports.CompLLMProvider, ports.CompToolchain}
	for _, comp := range components {
		cr, ok := report.Components[comp]
		if !ok {
			continue
		}
		cdr.renderComponent(ui, stderr, comp, cr)
		_, _ = fmt.Fprintln(stderr)
	}
}

// renderComponent renders a single component with its details
func (cdr *componentDetailRenderer) renderComponent(ui uiState, stderr io.Writer, comp ports.Component, cr ports.ComponentReport) {
	statusColor := colorGreen
	switch cr.Status {
	case ports.StatusUnhealthy:
		statusColor = colorRed
	case ports.StatusDegraded:
		statusColor = colorYellow
	}

	writeBestEffort(stderr, "%s[%s]%s %-12s : %s\n", ui.c(statusColor), strings.ToUpper(string(cr.Status)), ui.c(colorReset), comp, cr.Message)

	if cr.Details != nil {
		cdr.renderComponentDetails(ui, stderr, cr.Details)
	}
}

// renderComponentDetails renders the details map for a component
func (cdr *componentDetailRenderer) renderComponentDetails(ui uiState, stderr io.Writer, details interface{}) {
	if detailsMap, ok := details.(map[string]any); ok {
		for k, v := range detailsMap {
			if k == "binaries" {
				continue // Show binaries separately or handle specially
			}
			writeBestEffort(stderr, "    %s%s:%s %v\n", ui.c(colorGray), k, ui.c(colorReset), v)
		}

		if bins, ok := detailsMap["binaries"].(map[string]any); ok {
			bdr := &binaryDependencyRenderer{}
			bdr.renderBinaries(ui, stderr, bins)
		}
	}
}

// binaryDependencyRenderer handles binary version string formatting
type binaryDependencyRenderer struct{}

// renderBinaryInfo renders a single binary entry
func (bdr *binaryDependencyRenderer) renderBinaryInfo(ui uiState, stderr io.Writer, name string, info map[string]any) {
	ver := info["version_string"]
	if ver == nil || ver == "" {
		ver = "unknown version"
	}
	req := ""
	if r, ok := info["is_required"].(bool); ok && r {
		req = " (required)"
	}
	writeBestEffort(stderr, "    %s%s:%s %s%s\n", ui.c(colorGray), name, ui.c(colorReset), ver, req)
}

// renderBinaries renders the binaries map with version information
func (bdr *binaryDependencyRenderer) renderBinaries(ui uiState, stderr io.Writer, bins map[string]any) {
	for name, infoRaw := range bins {
		if info, ok := infoRaw.(map[string]any); ok {
			bdr.renderBinaryInfo(ui, stderr, name, info)
		}
	}
}

// NewRenderer creates a new ports.UIRenderer.
func NewRenderer(locker domain_security.Manager, stdout, stderr io.Writer, clk clock.Clock, metricsProvider ports.SystemMetricsProvider) ports.UIRenderer {
	if clk == nil {
		clk = clock.RealClock{}
	}
	if metricsProvider == nil {
		metricsProvider = &defaultMetricsProvider{}
	}
	tr, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)
	r := &stdUIRenderer{
		locker:          locker,
		stdout:          stdout,
		stderr:          stderr,
		clock:           clk,
		renderer:        tr,
		useColor:        true,
		metricsProvider: metricsProvider,
	}
	if err != nil {
		// ADR-007: Glamour failures are non-fatal. The system degrades gracefully
		// to raw text rendering. We explicitly nil the renderer so all downstream
		// code paths unambiguously detect the degraded state.
		r.renderer = nil
		r.LogSystemMessage(context.Background(), fmt.Sprintf("failed to initialize glamour renderer: %v", err), "warn")
	}
	return r
}

// sanitizeForTerminal converts common LaTeX/Math notation that LLMs use into terminal-friendly Unicode.
func sanitizeForTerminal(text string) string {
	replacements := map[string]string{
		"$\\leftrightarrow$": "↔",
		"$\\rightarrow$":     "→",
		"$\\leftarrow$":      "←",
		"$\\Rightarrow$":     "⇒",
		"$\\Leftarrow$":      "⇐",
		"$\\dots$":           "...",
		"$\\cdot$":           "·",
		"$\\times$":          "×",
		"$\\checkmark$":      "✓",
	}
	for old, new := range replacements {
		text = strings.ReplaceAll(text, old, new)
	}
	return text
}

// SetUseColor enables or disables ANSI color output.
func (r *stdUIRenderer) SetUseColor(use bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.useColor = use
}

// SetForceSpinner enables or disables forcing the spinner even in non-terminal environments (primarily for testing).
func (r *stdUIRenderer) SetForceSpinner(force bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forceSpinner = force
}

// SetWriters allows overriding the output writers (primarily for testing).
func (r *stdUIRenderer) SetWriters(stdout, stderr io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout = stdout
	r.stderr = stderr
}

// SetClock allows overriding the clock (primarily for testing).
func (r *stdUIRenderer) SetClock(clk clock.Clock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if clk == nil {
		clk = clock.RealClock{}
	}
	r.clock = clk
}

func (r *stdUIRenderer) getTimestamp() string {
	return r.getUIState().getTimestamp()
}

func (r *stdUIRenderer) nowSafe() time.Time {
	ui := r.getUIState()
	return ui.clock.Now()
}

func (r *stdUIRenderer) renderMarkdown(text string) {
	r.renderMarkdownWithUI(r.getUIState(), text)
}

func (r *stdUIRenderer) renderMarkdownWithUI(ui uiState, text string) {
	r.ioMu.Lock()
	defer r.ioMu.Unlock()
	r.renderMarkdownWithUILocked(ui, text)
}

func (r *stdUIRenderer) renderMarkdownWithUILocked(ui uiState, text string) {
	stdout := ui.stdout

	if r.renderer == nil {
		_, _ = fmt.Fprint(stdout, text)
		return
	}
	out, err := r.renderer.Render(text)
	if err != nil {
		r.logMarkdownRenderError(ui, err)
		_, _ = fmt.Fprint(stdout, text)
	} else {
		out = strings.TrimLeft(out, "\n")
		out = strings.TrimRight(out, "\n")
		if out != "" {
			_, _ = fmt.Fprint(stdout, out+"\n\n")
		}
	}
}

// logMarkdownRenderError logs a glamour rendering error at warn level.
// It uses a sync.Once to rate-limit: repeated failures produce at most
// one warning per renderer lifetime to avoid flooding stderr on every
// response chunk.
func (r *stdUIRenderer) logMarkdownRenderError(ui uiState, err error) {
	r.markdownErrOnce.Do(func() {
		writeBestEffort(ui.stderr, "\r%s%s[WARN] markdown rendering degraded, falling back to raw text: %v%s\n",
			ui.c(termClearLine), ui.c(colorYellow), err, ui.c(colorReset))
	})
}

type uiState struct {
	stdout   io.Writer
	stderr   io.Writer
	useColor bool
	clock    clock.Clock
}

func (s uiState) c(color string) string {
	if !s.useColor {
		return ""
	}
	return color
}

func (s uiState) getTimestamp() string {
	return s.clock.Now().Format("15:04:05")
}

func (r *stdUIRenderer) getUIState() uiState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stdout := r.stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := r.stderr
	if stderr == nil {
		stderr = io.Discard
	}
	return uiState{
		stdout:   stdout,
		stderr:   stderr,
		useColor: r.useColor,
		clock:    r.clock,
	}
}

func (r *stdUIRenderer) IsTerminalContext() bool {
	ui := r.getUIState()
	if f, ok := ui.stderr.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return true
	}
	return false
}

func (r *stdUIRenderer) printTokenLine(ui uiState, timestamp string, tokens int, maxTokens int, isActual bool, mode string) {
	stderr := ui.stderr
	tokenColor := colorReset
	if float64(tokens) > float64(maxTokens)*config.WarningRatio {
		tokenColor = colorYellow // Yellow caution
	}
	if float64(tokens) > float64(maxTokens) {
		tokenColor = colorRed // Red limit
	}

	modeStr := ""
	if mode != "" {
		modeStr = fmt.Sprintf(" - %s", mode)
	}

	prefix := "~"
	if isActual {
		prefix = ""
	}

	writeBestEffort(stderr, "%s[%s] Payload: %s%s%s%d%s/%d tokens%s%s\n",
		ui.c(colorGray), timestamp, prefix, ui.c(tokenColor), "", tokens, ui.c(colorGray), maxTokens, modeStr, ui.c(colorReset))
}

func (r *stdUIRenderer) formatFinalCost(status events.TurnStatus, ui uiState) string {
	if status.SessionCost <= 0 {
		return ""
	}

	hitRate := 0.0
	if total := status.TotalM + status.TotalH; total > 0 {
		hitRate = float64(status.TotalH) / float64(total) * 100
	}

	// Safe access to metrics; metrics could be nil if the turn stopped before inference
	turnCost := 0.0
	if status.Metrics != nil {
		turnCost = status.Metrics.Cost
	}

	// Format: (TurnCost TaskCost SessionCost DailyCost M: ... H: ... O: ...)
	return fmt.Sprintf(" %s($%.4f $%.4f %s$%.4f %s$%.4f%s M: %d H: %d %.1f%% O: %d)%s",
		ui.c(colorGray),
		turnCost, status.TaskCost,
		ui.c(colorGreen), status.SessionCost,
		ui.c(colorGray), status.DailyCost,
		ui.c(colorGray),
		status.TotalM,
		status.TotalH,
		hitRate,
		status.TotalO,
		ui.c(colorGray))
}

func (r *stdUIRenderer) renderFinalSummary(ui uiState, status events.TurnStatus) {
	stderr := ui.stderr
	costStr := r.formatFinalCost(status, ui)
	writeBestEffort(stderr, "%s╰─⠿ %sReady%s\n", ui.c(colorGray), ui.c(colorReset), costStr)
}

func (r *stdUIRenderer) RenderResponse(ctx context.Context, respContent *llm.Content, showThoughts, rawOutput bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	for _, part := range respContent.Parts {
		r.renderThoughtLocked(ui, part, showThoughts)
		r.renderTextLocked(ui, part, rawOutput)
		r.renderInlineDataLocked(ui, part)
	}
}

func (r *stdUIRenderer) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()
	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	stderr := ui.stderr

	writeBestEffort(stderr, "\n%s═══ System Health Diagnostic ═══%s\n\n", ui.c(colorBlue), ui.c(colorReset))

	r.renderOverallStatus(ui, stderr, report)
	r.renderComponents(ui, stderr, report)
}

// renderOverallStatus renders the header and overall status section
func (r *stdUIRenderer) renderOverallStatus(ui uiState, stderr io.Writer, report *ports.HealthReport) {
	hsr := &healthStatusRenderer{}
	hsr.renderOverallStatus(ui, stderr, report)
}

// renderComponents iterates through all components and renders each one
func (r *stdUIRenderer) renderComponents(ui uiState, stderr io.Writer, report *ports.HealthReport) {
	cdr := &componentDetailRenderer{}
	cdr.renderComponents(ui, stderr, report)
}

// IsRendererDegraded returns true if the glamour markdown renderer failed
// to initialize and the system is in raw-text fallback mode.
func (r *stdUIRenderer) IsRendererDegraded() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.renderer == nil
}
