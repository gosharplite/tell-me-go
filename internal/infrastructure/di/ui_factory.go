// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"fmt"
	"io"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui"
)

type uiFactory interface {
	UIRenderer() ports.UIRenderer
	HistoryRenderer() ports.HistoryRenderer
	HistoryBrowser() ports.HistoryBrowser
}

type defaultUIFactory struct {
	SM                    ConfigurableSecurityManager
	Stdout                io.Writer
	Stderr                io.Writer
	Logger                *slog.Logger
	SystemMetricsProvider ports.SystemMetricsProvider
}

func newUIFactory(sm ConfigurableSecurityManager, stdout, stderr io.Writer, logger *slog.Logger) uiFactory {
	return &defaultUIFactory{
		SM:                    sm,
		Stdout:                stdout,
		Stderr:                stderr,
		Logger:                logger,
		SystemMetricsProvider: telemetry.NewSystemMetricsProvider(),
	}
}

func (f *defaultUIFactory) UIRenderer() ports.UIRenderer {
	return ui.NewRenderer(f.SM, f.Stdout, f.Stderr, clock.RealClock{}, f.SystemMetricsProvider)
}

func (f *defaultUIFactory) HistoryRenderer() ports.HistoryRenderer {
	return &ui.StdHistoryRenderer{}
}

// tuiHistoryBrowser implements ports.HistoryBrowser using the TUI.
type tuiHistoryBrowser struct {
	logger *slog.Logger
}

// Browse launches the TUI history browser.
func (b *tuiHistoryBrowser) Browse(ctx stdctx.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	if closer, err := tui.InitLogger(); err == nil {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				b.logger.Warn("failed to close tui logger", "error", closeErr)
			}
		}()
	}

	model := tui.NewRootBrowserModel(ctx, provider, hManager)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui program error: %w", err)
	}
	return nil
}

func (f *defaultUIFactory) HistoryBrowser() ports.HistoryBrowser {
	return &tuiHistoryBrowser{
		logger: f.Logger,
	}
}
