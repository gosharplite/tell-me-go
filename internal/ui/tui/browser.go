// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)    // Blue
	modelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)    // Green
	thoughtStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true) // Gray
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))               // Yellow
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Padding(0, 1)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	warningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	archivedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	highlightStyle = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0")) // Yellow BG, Black FG
)

type historyLoadedMsg struct {
	dtos       []ports.HistoryViewDTO
	nextCursor string
	err        error
}

type fileChangedMsg struct{}

type watcherErrorMsg struct {
	err error
}

// rootBrowserModel implements the tea.Model interface for the history browser.
type rootBrowserModel struct {
	ctx              context.Context
	provider         ports.UnifiedHistoryProvider
	cmdService       ports.HistoryModifier
	history          []ports.HistoryViewDTO
	viewport         viewport.Model
	searchBar        textinput.Model
	matches          []int
	currentMatch     int
	selectedTurn     int
	isSearching      bool
	currentQuery     string
	isLoading        bool
	showThoughts     bool
	ready            bool
	cursor           string
	err              error
	width            int
	height           int
	lastMutationTime time.Time
	turnOffsets      []int

	// Render cache for expensive thought wrapping
	cachedThoughts map[string]string
	lastWidth      int
	lastQuery      string

	// watcherFactory allows tests to inject a failing watcher constructor.
	watcherFactory    func() (*fsnotify.Watcher, error)
	watcherErrorCount int // consecutive watcher error count for threshold alerting
	watcherRestarting bool
}

// NewRootBrowserModel creates a new history browser root model.
func NewRootBrowserModel(ctx context.Context, provider ports.UnifiedHistoryProvider, cmdService ports.HistoryModifier) *rootBrowserModel {
	ti := textinput.New()
	ti.Placeholder = "Search history..."
	ti.Prompt = "🔍 "

	return &rootBrowserModel{
		ctx:            ctx,
		provider:       provider,
		cmdService:     cmdService,
		searchBar:      ti,
		isLoading:      true,
		showThoughts:   true,
		selectedTurn:   -1,
		cachedThoughts: make(map[string]string),
		watcherFactory: fsnotify.NewWatcher,
	}
}

// Init initializes the model with an asynchronous disk read.
func (m *rootBrowserModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		fetchHistoryCmd(m.provider, ""),
		m.watchHistoryFileCmd(),
	)
}

func (m *rootBrowserModel) watchHistoryFileCmd() tea.Cmd {
	return func() tea.Msg {
		filepath := m.cmdService.GetFilePath()
		if filepath == "" {
			return nil
		}
		// Check if file exists to avoid watcher error
		if _, err := os.Stat(filepath); err != nil {
			if os.IsNotExist(err) {
				return nil // file not created yet, silently skip
			}
			log.Printf("cannot stat history file %s: %v", filepath, err)
			return watcherErrorMsg{err: fmt.Errorf("stat history file: %w", err)}
		}

		watcher, err := m.watcherFactory()
		if err != nil {
			log.Printf("failed to create history file watcher: %v", err)
			return watcherErrorMsg{err: fmt.Errorf("create watcher: %w", err)}
		}
		m.watcherErrorCount = 0 // reset on new watcher
		defer func() { _ = watcher.Close() }()

		if err := watcher.Add(filepath); err != nil {
			log.Printf("failed to add history file to watcher: %v", err)
			return watcherErrorMsg{err: fmt.Errorf("add watcher: %w", err)}
		}

		return m.processWatcherEvents(watcher)
	}
}

func (m *rootBrowserModel) processWatcherEvents(watcher *fsnotify.Watcher) tea.Msg {
	select {
	case <-m.ctx.Done():
		return nil
	case event, ok := <-watcher.Events:
		return m.handleWatcherEvent(event, ok)
	case err, ok := <-watcher.Errors:
		return m.handleWatcherError(err, ok)
	}
}

func (m *rootBrowserModel) handleWatcherEvent(event fsnotify.Event, ok bool) tea.Msg {
	if !ok {
		return nil
	}
	m.watcherErrorCount = 0 // reset consecutive error counter on successful event
	if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		return fileChangedMsg{}
	}
	return nil
}

func (m *rootBrowserModel) handleWatcherError(err error, ok bool) tea.Msg {
	if !ok {
		return nil
	}
	log.Printf("history file watcher error: %v", err)
	m.watcherErrorCount++
	if m.watcherErrorCount >= 3 {
		return watcherErrorMsg{err: fmt.Errorf("file watcher failed after %d errors: %w", m.watcherErrorCount, err)}
	}
	return nil
}

// Update handles incoming messages and updates the model state.
func (m *rootBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case historyLoadedMsg:
		return m.handleHistoryLoadedMsg(msg)
	case fileChangedMsg:
		return m.handleFileChangedMsg(msg)
	case watcherErrorMsg:
		m.err = msg.err
		return m, nil
	}

	return m.handleViewportUpdate(msg)
}

func (m *rootBrowserModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isSearching {
		return m.handleSearchInput(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "k", "n", "N":
		return m.handleNavigationKeys(msg)
	case "p", "r", " ", "/":
		return m.handleActionKeys(msg)
	}

	return m.handleViewportUpdate(msg)
}

func (m *rootBrowserModel) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "enter":
		m.isSearching = false
		if m.currentQuery != m.searchBar.Value() {
			m.cachedThoughts = make(map[string]string)
			m.lastQuery = m.searchBar.Value()
		}
		m.currentQuery = m.searchBar.Value()
		m.currentMatch = 0
		m.updateViewportContent()
		m.updateViewportHeight()
		m.viewport.GotoTop()
		return m, nil
	case "esc":
		m.isSearching = false
		m.searchBar.SetValue("")
		if m.currentQuery != "" {
			m.cachedThoughts = make(map[string]string)
			m.lastQuery = ""
		}
		m.currentQuery = ""
		m.matches = nil
		m.updateViewportContent()
		m.updateViewportHeight()
		return m, nil
	}
	m.searchBar, cmd = m.searchBar.Update(msg)
	return m, cmd
}

func (m *rootBrowserModel) handleNavigationKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j":
		m.moveSelection(1)
	case "k":
		m.moveSelection(-1)
	case "n":
		m.moveSearchMatch(1)
	case "N":
		m.moveSearchMatch(-1)
	}
	return m, nil
}

func (m *rootBrowserModel) moveSelection(delta int) {
	if len(m.history) == 0 {
		return
	}
	m.selectedTurn += delta
	if m.selectedTurn < 0 {
		m.selectedTurn = 0
	} else if m.selectedTurn >= len(m.history) {
		m.selectedTurn = len(m.history) - 1
	}
	m.updateViewportContent()
	m.syncViewportToSelectedTurn()
}

func (m *rootBrowserModel) moveSearchMatch(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.currentMatch = (m.currentMatch + delta) % len(m.matches)
	if m.currentMatch < 0 {
		m.currentMatch = len(m.matches) - 1
	}
	m.viewport.SetYOffset(m.matches[m.currentMatch])
}

func (m *rootBrowserModel) handleActionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "p":
		m.togglePin()
		if m.isLoading {
			return m, fetchHistoryCmd(m.provider, "")
		}
		return m, nil
	case "r":
		cmd := m.rollbackToSelected()
		return m, cmd
	case " ":
		m.showThoughts = !m.showThoughts
		m.updateViewportContent()
		return m, nil
	case "/":
		m.isSearching = true
		m.searchBar.Focus()
		m.updateViewportHeight()
		return m, nil
	}
	return m, nil
}

func (m *rootBrowserModel) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	if m.width != msg.Width {
		m.cachedThoughts = make(map[string]string)
		m.lastWidth = msg.Width
	}
	m.width = msg.Width
	m.height = msg.Height
	if m.width < 20 {
		m.width = 20
	}
	if m.height < 5 {
		m.height = 5
	}
	if !m.ready {
		m.viewport = viewport.New(msg.Width, msg.Height-m.calculateFooterHeight())
		m.updateViewportContent()
		m.ready = true
	} else {
		m.viewport.Width = msg.Width
		m.updateViewportHeight()
		m.updateViewportContent()
	}
	return m.handleViewportUpdate(msg)
}

func (m *rootBrowserModel) handleHistoryLoadedMsg(msg historyLoadedMsg) (tea.Model, tea.Cmd) {
	m.isLoading = false

	// If we have data, process it even when there's an error.
	// Only treat the error as blocking if no data was returned.
	if msg.err != nil && len(msg.dtos) == 0 {
		log.Printf("failed to load history: %v", msg.err)
		m.err = msg.err
		return m, nil
	}

	if msg.err != nil {
		// Partial result: log the error but still display what we got.
		log.Printf("partial history load (got %d items): %v", len(msg.dtos), msg.err)
		// DO NOT set m.err — let the data display. The user can retry by scrolling.
	}

	isInitialLoad := (m.selectedTurn == -1)

	if len(msg.dtos) > 0 {
		if isInitialLoad {
			m.history = msg.dtos
			m.selectedTurn = len(m.history) - 1
		} else {
			// Prepend older history
			numAdded := len(msg.dtos)
			m.history = append(msg.dtos, m.history...)

			// Update viewport and maintain scroll position
			m.updateViewportContent()
			addedLines := 0
			if numAdded < len(m.turnOffsets) {
				addedLines = m.turnOffsets[numAdded]
			}
			m.viewport.SetYOffset(m.viewport.YOffset + addedLines)

			// Adjust selected turn
			m.selectedTurn += numAdded
		}
	}

	m.cursor = msg.nextCursor
	m.updateViewportContent()
	m.updateViewportHeight()

	if isInitialLoad && len(m.history) > 0 {
		m.viewport.GotoBottom()
	}

	return m, nil
}

func (m *rootBrowserModel) handleFileChangedMsg(msg fileChangedMsg) (tea.Model, tea.Cmd) {
	// Debounce: ignore changes if we just mutated the file
	if time.Since(m.lastMutationTime) < 500*time.Millisecond {
		if m.watcherRestarting {
			return m, nil
		}
		m.watcherRestarting = true
		return m, m.watchHistoryFileCmd()
	}
	m.watcherRestarting = false
	// Refresh active memory
	m.history = nil
	m.cursor = ""
	m.isLoading = true
	return m, tea.Batch(
		fetchHistoryCmd(m.provider, ""),
		m.watchHistoryFileCmd(),
	)
}

// View renders the current state of the model.
func (m *rootBrowserModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\nPress 'q' to quit.", m.err))
	}

	if !m.ready {
		return "Initializing terminal..."
	}

	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")

	if m.isSearching {
		sb.WriteString(footerStyle.Render(m.searchBar.View()))
	} else {
		sb.WriteString(m.renderFooter())
	}

	return sb.String()
}
