// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	stdctx "context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/ui/tui/editor"
)

// ProgramRunner abstracts tea.Program.Run to allow fault injection in tests.
type ProgramRunner interface {
	Run() (tea.Model, error)
}

// HistoryEditor launches the TUI turn editor for the last model turn.
type HistoryEditor struct {
	initLogger func() (io.Closer, error)
	newProgram func(model tea.Model, opts ...tea.ProgramOption) ProgramRunner
}

// NewHistoryEditor creates a new HistoryEditor.
func NewHistoryEditor(initLogger func() (io.Closer, error), newProgram func(model tea.Model, opts ...tea.ProgramOption) ProgramRunner) *HistoryEditor {
	return &HistoryEditor{
		initLogger: initLogger,
		newProgram: newProgram,
	}
}

// Edit launches the TUI turn editor. Implements ports.HistoryEditor.
func (e *HistoryEditor) Edit(ctx stdctx.Context, hManager ports.HistoryManager) error {
	if closer, err := e.initLogger(); err == nil {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				_ = closeErr
			}
		}()
	}

	index, content, err := hManager.GetLastModelTurn(ctx)
	if err != nil {
		return fmt.Errorf("get last model turn: %w", err)
	}

	var text, thought string
	for _, p := range content.Parts {
		if p.IsThought {
			thought += p.Text
		} else if p.Text != "" {
			text += p.Text
		}
	}

	model := editor.NewModel(text, thought)
	p := e.newProgram(model, tea.WithAltScreen())
	result, runErr := p.Run()
	if runErr != nil {
		return fmt.Errorf("tui editor error: %w", runErr)
	}

	ed := result.(*editor.EditorModel)
	if ed.WasAborted() {
		return stdctx.Canceled
	}

	return hManager.UpdateTurnContent(ctx, index, ed.EditedText(), ed.EditedThought())
}
