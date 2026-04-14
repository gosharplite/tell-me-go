// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
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
	}
}

// Init initializes the model with an asynchronous disk read.
func (m *rootBrowserModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		fetchHistoryCmd(m.provider, ""),
		watchHistoryFileCmd(m.ctx, m.cmdService.GetFilePath()),
	)
}

func watchHistoryFileCmd(ctx context.Context, filepath string) tea.Cmd {
	return func() tea.Msg {
		if filepath == "" {
			return nil
		}
		// Check if file exists to avoid watcher error
		if _, err := os.Stat(filepath); os.IsNotExist(err) {
			return nil
		}

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("failed to create history file watcher: %v", err)
			return nil
		}
		defer func() { _ = watcher.Close() }()

		if err := watcher.Add(filepath); err != nil {
			log.Printf("failed to add history file to watcher: %v", err)
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				return fileChangedMsg{}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("history file watcher error: %v", err)
		}
		return nil
	}
}

func (m *rootBrowserModel) updateViewportHeight() {
	if !m.ready {
		return
	}
	m.viewport.Height = m.height - m.calculateFooterHeight()
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
		if len(m.history) > 0 {
			m.selectedTurn++
			if m.selectedTurn >= len(m.history) {
				m.selectedTurn = len(m.history) - 1
			}
			m.updateViewportContent()
			m.syncViewportToSelectedTurn()
		}
	case "k":
		if len(m.history) > 0 {
			m.selectedTurn--
			if m.selectedTurn < 0 {
				m.selectedTurn = 0
			}
			m.updateViewportContent()
			m.syncViewportToSelectedTurn()
		}
	case "n":
		if len(m.matches) > 0 {
			m.currentMatch = (m.currentMatch + 1) % len(m.matches)
			m.viewport.SetYOffset(m.matches[m.currentMatch])
		}
	case "N":
		if len(m.matches) > 0 {
			m.currentMatch--
			if m.currentMatch < 0 {
				m.currentMatch = len(m.matches) - 1
			}
			m.viewport.SetYOffset(m.matches[m.currentMatch])
		}
	}
	return m, nil
}

func (m *rootBrowserModel) handleActionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "p":
		m.togglePin()
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
	if msg.err != nil {
		log.Printf("failed to load history: %v", msg.err)
		m.err = msg.err
		return m, nil
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
			addedLines := m.turnOffsets[numAdded]
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
		return m, watchHistoryFileCmd(m.ctx, m.cmdService.GetFilePath())
	}
	// Refresh active memory
	m.history = nil
	m.cursor = ""
	m.isLoading = true
	return m, tea.Batch(
		fetchHistoryCmd(m.provider, ""),
		watchHistoryFileCmd(m.ctx, m.cmdService.GetFilePath()),
	)
}

func (m *rootBrowserModel) handleViewportUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// Forward messages to the viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	// Infinite pagination trigger (Older history from archive)
	if m.viewport.YOffset == 0 && !m.isLoading && m.cursor != "" && m.cursor != "EOF" && m.ready && !m.isSearching {
		m.isLoading = true
		m.updateViewportContent()
		return m, tea.Batch(append(cmds, fetchHistoryCmd(m.provider, m.cursor))...)
	}

	return m, tea.Batch(cmds...)
}

func (m *rootBrowserModel) updateViewportContent() {
	// 1. Invalidate cache if needed
	if m.lastWidth != m.width || m.lastQuery != m.currentQuery {
		m.cachedThoughts = make(map[string]string)
		m.lastWidth = m.width
		m.lastQuery = m.currentQuery
	}

	// 2. Populate cache for all history turns
	for _, dto := range m.history {
		if _, ok := m.cachedThoughts[dto.ID]; !ok && m.showThoughts && dto.ThoughtProcess != "" {
			m.cachedThoughts[dto.ID] = m.preRenderThought(dto)
		}
	}

	// 3. Render and update state
	rendered, offsets := m.renderHistory()
	m.turnOffsets = offsets
	m.recalculateSearchMatches(rendered)
	m.viewport.SetContent(rendered)
}

func (m *rootBrowserModel) preRenderThought(dto ports.HistoryViewDTO) string {
	thoughtText := "💭 [THOUGHTS]\n" + dto.ThoughtProcess
	if m.currentQuery != "" {
		thoughtText = m.highlightMatches(thoughtText, m.currentQuery)
	}

	prefixLen := 2 // standard prefix "  " or "> "
	maxWidth := m.width - prefixLen
	if maxWidth < 20 {
		maxWidth = 20
	}

	return thoughtStyle.Width(maxWidth).Render(thoughtText)
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

func (m *rootBrowserModel) renderHistory() (string, []int) {
	if len(m.history) == 0 && m.isLoading {
		return "Loading history...", nil
	}
	if len(m.history) == 0 && m.cursor == "EOF" {
		return "No history found.", nil
	}

	turnOffsets := make([]int, 0, len(m.history))
	var sb strings.Builder

	if m.isLoading {
		sb.WriteString(thoughtStyle.Render("Loading more messages...") + "\n\n")
	} else if m.cursor == "EOF" && len(m.history) > 0 {
		sb.WriteString(archivedStyle.Render("─── Start of History ───") + "\n\n")
	}

	offset := m.getSystemOffset()

	for i, dto := range m.history {
		turnOffsets = append(turnOffsets, strings.Count(sb.String(), "\n"))

		prefix := "  "
		if i == m.selectedTurn {
			prefix = "> "
		}

		sb.WriteString(m.renderTurnHeader(dto, i == m.selectedTurn, offset))

		if m.showThoughts && dto.ThoughtProcess != "" {
			sb.WriteString(m.renderThoughts(dto, prefix))
		}

		if len(dto.ToolCalls) > 0 {
			sb.WriteString(m.renderToolCalls(dto, prefix))
		}

		sb.WriteString(m.renderContent(dto, prefix))

		if i < len(m.history)-1 {
			sb.WriteString(m.renderSeparator())
		}
	}

	return sb.String(), turnOffsets
}

func (m *rootBrowserModel) renderTurnHeader(dto ports.HistoryViewDTO, isSelected bool, offset int) string {
	prefix := "  "
	if isSelected {
		prefix = "> "
	}

	roleLabel := strings.ToUpper(dto.Role)
	if dto.Role == "assistant" {
		roleLabel = "MODEL"
	}

	turnStr := ""
	if !dto.IsArchived && (dto.OriginalIndex >= offset || dto.Role != "system") {
		turnStr = fmt.Sprintf(" - %d", ((dto.OriginalIndex-offset)/2)+1)
	}

	var styledLabel string
	switch dto.Role {
	case "user":
		styledLabel = userStyle.Render(fmt.Sprintf("[%s]%s", roleLabel, turnStr))
	case "assistant", "model":
		styledLabel = modelStyle.Render(fmt.Sprintf("[%s]%s", roleLabel, turnStr))
	default:
		styledLabel = lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("[%s]%s", roleLabel, turnStr))
	}

	if dto.IsArchived {
		styledLabel += archivedStyle.Render(" (archived)")
	}
	if dto.IsPinned {
		styledLabel += lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true).Render(" [PINNED]")
	}

	return prefix + styledLabel + "\n"
}

func (m *rootBrowserModel) renderThoughts(dto ports.HistoryViewDTO, prefix string) string {
	var wrappedText string

	// Pure read from cache
	if cached, ok := m.cachedThoughts[dto.ID]; ok {
		wrappedText = cached
	} else {
		// Computation only, no storage in m.cachedThoughts to keep this pure
		thoughtText := "💭 [THOUGHTS]\n" + dto.ThoughtProcess
		if m.currentQuery != "" {
			thoughtText = m.highlightMatches(thoughtText, m.currentQuery)
		}

		maxWidth := m.width - len(prefix)
		if maxWidth < 20 {
			maxWidth = 20
		}

		wrappedText = thoughtStyle.Width(maxWidth).Render(thoughtText)
	}

	// Apply the dynamic prefix
	var sb strings.Builder
	lines := strings.Split(wrappedText, "\n")
	for _, line := range lines {
		sb.WriteString(prefix)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

func (m *rootBrowserModel) renderToolCalls(dto ports.HistoryViewDTO, prefix string) string {
	var sb strings.Builder
	for _, tool := range dto.ToolCalls {
		sb.WriteString(prefix)
		sb.WriteString(toolStyle.Render(fmt.Sprintf("  🔧 Executing tool: %s", tool)))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func (m *rootBrowserModel) renderContent(dto ports.HistoryViewDTO, prefix string) string {
	content := dto.ContentPreview
	if m.currentQuery != "" {
		content = m.highlightMatches(content, m.currentQuery)
	}

	var sb strings.Builder
	lines := strings.Split(content, "\n")
	for j, line := range lines {
		sb.WriteString(prefix)
		sb.WriteString(line)
		if j < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m *rootBrowserModel) renderSeparator() string {
	return "\n\n" + archivedStyle.Render(strings.Repeat("─", m.width/2)) + "\n\n"
}

func (m *rootBrowserModel) recalculateSearchMatches(rendered string) {
	m.matches = []int{}
	if m.currentQuery != "" {
		re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(m.currentQuery))
		if err == nil {
			lines := strings.Split(rendered, "\n")
			for i, line := range lines {
				if re.MatchString(line) {
					m.matches = append(m.matches, i)
				}
			}
		}
	}

	if len(m.matches) > 0 {
		if m.currentMatch >= len(m.matches) {
			m.currentMatch = len(m.matches) - 1
		}
	} else {
		m.currentMatch = 0
	}
}

func (m *rootBrowserModel) highlightMatches(text, query string) string {
	if query == "" {
		return text
	}

	// Case-insensitive regex for the query
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return text
	}

	return re.ReplaceAllStringFunc(text, func(match string) string {
		return highlightStyle.Render(match)
	})
}

func (m *rootBrowserModel) calculateFooterHeight() int {
	if m.isSearching {
		return 1
	}

	activeTurns, pinnedTurns := m.getPinningMetrics()

	if pinnedTurns > 5 || (activeTurns > 0 && float64(pinnedTurns)/float64(activeTurns) > 0.5) {
		return 2
	}
	return 1
}

func (m *rootBrowserModel) renderFooter() string {
	var sb strings.Builder

	activeTurns, pinnedTurns := m.getPinningMetrics()

	if pinnedTurns > 5 || (activeTurns > 0 && float64(pinnedTurns)/float64(activeTurns) > 0.5) {
		sb.WriteString(warningStyle.Render("⚠️ High Pinning Pressure: Auto-summarization may fail."))
		sb.WriteString("\n")
	}

	thoughtsStatus := "ON"
	if !m.showThoughts {
		thoughtsStatus = "OFF"
	}
	fmt.Fprintf(&sb, "↑/↓: Scroll • Space: Thoughts [%s] • /: Search • j/k: Select • p: Pin • r: Rollback • q: Quit", thoughtsStatus)

	if m.currentQuery != "" {
		matchInfo := ""
		if len(m.matches) > 0 {
			matchInfo = fmt.Sprintf(" (%d/%d matches)", m.currentMatch+1, len(m.matches))
		} else {
			matchInfo = " (no matches)"
		}
		fmt.Fprintf(&sb, " • Query: %q%s", m.currentQuery, matchInfo)
	}
	if m.isLoading {
		sb.WriteString(" • LOADING...")
	}
	return footerStyle.Render(sb.String())
}

func fetchHistoryCmd(provider ports.UnifiedHistoryProvider, cursor string) tea.Cmd {
	return func() tea.Msg {
		dtos, nextCursor, err := provider.GetHistoryStream(context.Background(), 20, cursor)
		if err == nil && len(dtos) == 0 && cursor != "" {
			nextCursor = "EOF"
		}
		return historyLoadedMsg{
			dtos:       dtos,
			nextCursor: nextCursor,
			err:        err,
		}
	}
}

func (m *rootBrowserModel) syncViewportToSelectedTurn() {
	if m.selectedTurn >= 0 && m.selectedTurn < len(m.turnOffsets) {
		targetLine := m.turnOffsets[m.selectedTurn]
		if targetLine < m.viewport.YOffset {
			m.viewport.SetYOffset(targetLine)
		}
		if targetLine >= m.viewport.YOffset+m.viewport.Height {
			m.viewport.SetYOffset(targetLine - m.viewport.Height + 1)
		}
	}
}

func (m *rootBrowserModel) getSystemOffset() int {
	for _, dto := range m.history {
		if dto.OriginalIndex == 0 && dto.Role == "system" {
			return 1
		}
	}
	return 0
}

func (m *rootBrowserModel) getPinningMetrics() (activeTurns int, pinnedTurns int) {
	offset := m.getSystemOffset()
	lastTurnIdx := -1
	for _, dto := range m.history {
		if dto.IsArchived {
			continue
		}
		if dto.OriginalIndex < offset && dto.Role == "system" {
			continue
		}
		turnIdx := (dto.OriginalIndex - offset) / 2
		if turnIdx != lastTurnIdx {
			activeTurns++
			if dto.IsPinned {
				pinnedTurns++
			}
			lastTurnIdx = turnIdx
		}
	}
	return activeTurns, pinnedTurns
}

func (m *rootBrowserModel) togglePin() {
	if m.selectedTurn == -1 || m.selectedTurn >= len(m.history) {
		return
	}

	dto := m.history[m.selectedTurn]
	if dto.IsArchived {
		return
	}

	offset := m.getSystemOffset()
	if dto.OriginalIndex < offset && dto.Role == "system" {
		return // System message cannot be pinned/unpinned manually via turn index
	}

	// Toggle pin state
	err := m.cmdService.SetPinned(context.Background(), (dto.OriginalIndex-offset)/2, !dto.IsPinned)
	if err != nil {
		m.err = err
		return
	}

	// Update local DTOs for the turn to reflect toggle
	newPinState := !dto.IsPinned
	turnStartIdx := ((dto.OriginalIndex - offset) & ^1) + offset
	for idx := range m.history {
		if !m.history[idx].IsArchived && (m.history[idx].OriginalIndex & ^1) == (turnStartIdx & ^1) {
			// Actually we should match the exact turn start index.
			// The original logic 'dto.OriginalIndex & ^1' was simpler but offset-unaware.
			// Let's use a more precise check.
			msgOffset := m.history[idx].OriginalIndex - offset
			if msgOffset >= 0 && (msgOffset&^1) == (dto.OriginalIndex-offset)&^1 {
				m.history[idx].IsPinned = newPinState
			}
		}
	}
	m.lastMutationTime = time.Now()
	m.updateViewportContent()
	m.updateViewportHeight()
}

func (m *rootBrowserModel) rollbackToSelected() tea.Cmd {
	if m.selectedTurn == -1 || m.selectedTurn >= len(m.history) {
		return nil
	}

	dto := m.history[m.selectedTurn]
	if dto.IsArchived {
		return nil
	}

	// We need total active turns.
	// Let's get it from the last active DTO.
	lastActiveIdx := -1
	for i := len(m.history) - 1; i >= 0; i-- {
		if !m.history[i].IsArchived {
			lastActiveIdx = m.history[i].OriginalIndex
			break
		}
	}
	if lastActiveIdx == -1 {
		return nil
	}

	offset := m.getSystemOffset()
	totalMsgs := lastActiveIdx + 1

	// If user selected the system message, rollback everything after it.
	targetStartIdx := dto.OriginalIndex
	if targetStartIdx >= offset {
		targetStartIdx = ((dto.OriginalIndex - offset) & ^1) + offset
	}

	turnsToRemove := (totalMsgs - targetStartIdx + 1) / 2

	_, _, _, err := m.cmdService.RollbackTurns(context.Background(), turnsToRemove)
	if err != nil {
		m.err = err
		return nil
	}

	m.lastMutationTime = time.Now()
	// Full Refresh
	m.history = nil
	m.cursor = ""
	m.selectedTurn = -1
	m.isLoading = true
	return fetchHistoryCmd(m.provider, "")
}
