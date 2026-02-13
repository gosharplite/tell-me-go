// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ICapturer defines the interface for UI interactions that the Orchestrator needs.
type ICapturer interface {
	IsTTY(v any) bool
}

// Orchestrator manages the session lifecycle and agent execution.
type Orchestrator struct {
	HomeDir         string
	Version         string
	Loader          config.ConfigLoader
	SM              domain_security.ISecurityManager
	Stdout          io.Writer
	Stderr          io.Writer
	AgentFactory    AgentFactory
	HistoryRenderer services.HistoryRenderer
	UIRenderer      services.UIRenderer
}

type AgentFactory func(loader config.ConfigLoader, client domain_llm.LLMGateway, hManager services.HistoryManager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) Chatter

// SessionConfig contains configuration for a single session execution.
type SessionConfig struct {
	ConfigPath string
	NewSession bool
	LastN      int
	RawOutput  bool
	Prompt     string
	Config     *config.Config
}

// SessionDependencies holds the required components for a session.
type SessionDependencies struct {
	Paths            *persistence.Paths
	HistoryManager   services.HistoryManager
	Client           domain_llm.LLMClient
	Gateway          domain_llm.LLMGateway
	Registry         domaintools.IToolRegistry
	Tracker          domain_pricing.ICostTracker
	PricingData      domain_pricing.PricingData
	PricingOverrides map[string]domain_pricing.ModelPricing
	EventBus         events.EventBus
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(homeDir, version string, loader config.ConfigLoader, sm domain_security.ISecurityManager, stdout, stderr io.Writer, agentFactory AgentFactory, historyRenderer services.HistoryRenderer, uiRenderer services.UIRenderer) *Orchestrator {
	return &Orchestrator{
		HomeDir:         homeDir,
		Version:         version,
		Loader:          loader,
		SM:              sm,
		Stdout:          stdout,
		Stderr:          stderr,
		AgentFactory:    agentFactory,
		HistoryRenderer: historyRenderer,
		UIRenderer:      uiRenderer,
	}
}

// Run executes the session orchestration.
func (o *Orchestrator) Run(ctx context.Context, sCfg *SessionConfig, deps *SessionDependencies, capturer ICapturer) error {
	isTTY := capturer.IsTTY(o.Stdout)
	o.renderHistory(deps.HistoryManager, sCfg, isTTY)

	if sCfg.Prompt == "" && sCfg.LastN > 0 {
		return nil
	}

	chatAgent := o.AgentFactory(o.Loader, deps.Gateway, deps.HistoryManager, deps.Registry, o.SM, sCfg.Config.DisableStreaming, deps.EventBus, sCfg.Config.Model, sCfg.Config.Mode, deps.Paths.LogPath, deps.PricingOverrides, deps.Tracker)
	defer func() {
		if err := chatAgent.Shutdown(ctx); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}
	}()

	if err := o.applyConfiguration(ctx, chatAgent, sCfg, deps.Paths, deps.PricingData, capturer); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	sess := NewSession(sessionID, deps.HistoryManager)
	if err := chatAgent.Chat(ctx, sess, sCfg.Prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return nil
}

func (o *Orchestrator) renderHistory(hManager services.HistoryManager, sCfg *SessionConfig, isTTY bool) {
	if sCfg.LastN <= 0 {
		return
	}
	o.HistoryRenderer.Render(o.Stdout, hManager, sCfg.LastN, services.HistoryRenderOptions{
		Raw:          sCfg.RawOutput,
		ShowThoughts: sCfg.Config.ShowThoughts,
		UseColor:     isTTY && !sCfg.RawOutput,
	})
}

func (o *Orchestrator) applyConfiguration(ctx context.Context, chatAgent Chatter, sCfg *SessionConfig, paths *persistence.Paths, pData domain_pricing.PricingData, capturer ICapturer) error {
	o.setupUIRendering(chatAgent, sCfg.Config, sCfg.RawOutput, paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, sCfg.Config.MaxToolTurns, sCfg.Config.ResolveContextWindow(), sCfg.Config.MaxHistoryTurns); err != nil {
		return err
	}
	return chatAgent.SetTieredThreshold(ctx, sCfg.Config.ResolveTieredThreshold(pData))
}

func (o *Orchestrator) setupUIRendering(chatAgent Chatter, cfg *config.Config, rawOutput bool, logPath string, capturer ICapturer) {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := newUIBridge(o.UIRenderer, cfg.ShowThoughts, cfg.ShowTools, rawOutput, useColor, logPath)
	chatAgent.Subscribe(bridge.handleEvent)
}

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	renderer     services.UIRenderer
	showThoughts bool
	showTools    bool
	rawOutput    bool
	useColor     bool
	logFile      string
}

// newUIBridge creates a new uiBridge.
func newUIBridge(renderer services.UIRenderer, showThoughts, showTools, rawOutput, useColor bool, logFile string) *uiBridge {
	return &uiBridge{
		renderer:     renderer,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		useColor:     useColor,
		logFile:      logFile,
	}
}

// handleEvent processes a domain event and updates the UI.
func (b *uiBridge) handleEvent(e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		b.renderer.LogTurnStatus(ev.Status)
	case events.ResponseStreamEvent:
		ctx := b.ensureContext(ev.Context, "ResponseStreamEvent")
		uiCh, uiFinalize := b.renderer.StreamResponse(ctx, b.showThoughts, b.rawOutput)
		b.relayStream(ctx, ev.Stream, uiCh)
		_ = uiFinalize()
	case events.UsageMetricsEvent:
		ctx := b.ensureContext(ev.Context, "UsageMetricsEvent")
		b.renderer.LogUsage(ctx, ev.Metrics, b.logFile, ev.StartTime)
	case events.ToolCallEvent:
		b.renderer.LogToolCall(ev.Calls, ev.Turn, ev.MaxTurns, b.showTools)
	case events.ToolResultEvent:
		b.renderer.LogToolResult(ev.Name, ev.Result, b.showTools)
	case events.SystemMessageEvent:
		b.renderer.LogSystemMessage(ev.Message, ev.Level)
	case events.StatusUpdate:
		b.renderer.LogSystemMessage(ev.Message, ev.Level)
	}
}

func (b *uiBridge) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		b.renderer.LogSystemMessage(name+" missing context", "warn")
		return context.Background()
	}
	return ctx
}

func (b *uiBridge) relayStream(ctx context.Context, stream <-chan *domain_llm.Content, uiCh chan<- *domain_llm.Content) {
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-stream:
			if !ok {
				return
			}
			select {
			case uiCh <- c:
			case <-ctx.Done():
				return
			}
		}
	}
}
