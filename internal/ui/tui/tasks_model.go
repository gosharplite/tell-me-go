// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// tasksLoadedMsg is a custom message carrying the result of a task fetch.
type tasksLoadedMsg struct {
	tasks      []ports.Task
	totalCount int
	err        error
}

// normalizeStatusFilter normalizes a free-text input into a valid status filter.
// Returns "pending", "completed", or "" (meaning "all") for any unrecognized input.
func normalizeStatusFilter(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "pending", "completed":
		return input
	default:
		return ""
	}
}

// taskListModel implements tea.Model for the task list browser.
type taskListModel struct {
	ctx          context.Context
	provider     ports.TaskStore
	viewport     viewport.Model
	searchBar    textinput.Model
	tasks        []ports.Task
	selected     int
	totalCount   int
	pageOffset   int
	pageSize     int
	statusFilter string
	ready        bool
	width        int
	height       int
	err          error
}

// NewTaskListModel creates a new task list model.
func newTaskListModel(ctx context.Context, provider ports.TaskStore) *taskListModel {
	ti := textinput.New()
	ti.Placeholder = "Status filter (pending/completed)..."
	ti.Prompt = "📋 "

	return &taskListModel{
		ctx:          ctx,
		provider:     provider,
		searchBar:    ti,
		selected:     -1,
		pageOffset:   0,
		pageSize:     50,
		statusFilter: "",
	}
}

// Init initializes the model with a blink command and the initial task fetch.
func (m *taskListModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		fetchTasksCmd(m.provider, "", m.pageOffset, m.pageSize),
	)
}

// fetchTasksCmd returns a command that asynchronously fetches tasks from the provider.
func fetchTasksCmd(provider ports.TaskStore, status string, offset, limit int) tea.Cmd {
	return func() tea.Msg {
		tasks := provider.ListTasks(status, limit, offset)
		count := provider.CountTasks(status)
		return tasksLoadedMsg{tasks: tasks, totalCount: count}
	}
}

// Update handles incoming messages and updates the model state.
func (m *taskListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case tasksLoadedMsg:
		return m.handleTasksLoadedMsg(msg)
	}

	return m.handleViewportUpdate(msg)
}

func (m *taskListModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchBar.Focused() {
		return m.handleSearchInput(msg)
	}

	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit
	case "j":
		m.moveSelection(1)
		return m, nil
	case "k":
		m.moveSelection(-1)
		return m, nil
	case "/":
		m.searchBar.Focus()
		m.updateViewportHeight()
		return m, nil
	case "n":
		m.nextPage()
		m.selected = 0
		return m, fetchTasksCmd(m.provider, m.statusFilter, m.pageOffset, m.pageSize)
	case "p":
		m.prevPage()
		m.selected = 0
		return m, fetchTasksCmd(m.provider, m.statusFilter, m.pageOffset, m.pageSize)
	}

	return m.handleViewportUpdate(msg)
}

func (m *taskListModel) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "enter":
		m.searchBar.Blur()
		m.pageOffset = 0
		m.selected = -1
		m.statusFilter = normalizeStatusFilter(m.searchBar.Value())
		m.updateViewportHeight()
		return m, fetchTasksCmd(m.provider, m.statusFilter, m.pageOffset, m.pageSize)
	case "esc":
		m.searchBar.SetValue("")
		m.searchBar.Blur()
		m.pageOffset = 0
		m.selected = -1
		m.statusFilter = ""
		m.updateViewportHeight()
		return m, fetchTasksCmd(m.provider, "", m.pageOffset, m.pageSize)
	}
	m.searchBar, cmd = m.searchBar.Update(msg)
	return m, cmd
}

func (m *taskListModel) moveSelection(delta int) {
	if len(m.tasks) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	} else if m.selected >= len(m.tasks) {
		m.selected = len(m.tasks) - 1
	}
	m.updateViewportContent()
}

func (m *taskListModel) nextPage() {
	next := m.pageOffset + m.pageSize
	if next < m.totalCount {
		m.pageOffset = next
	}
}

func (m *taskListModel) prevPage() {
	m.pageOffset -= m.pageSize
	if m.pageOffset < 0 {
		m.pageOffset = 0
	}
}

func (m *taskListModel) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
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

func (m *taskListModel) handleTasksLoadedMsg(msg tasksLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}

	isInitialLoad := (m.selected == -1)

	m.tasks = msg.tasks
	m.totalCount = msg.totalCount

	if isInitialLoad && len(m.tasks) > 0 {
		m.selected = 0
	}

	m.updateViewportContent()
	m.updateViewportHeight()

	return m, nil
}

func (m *taskListModel) handleViewportUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View renders the current state of the model.
func (m *taskListModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\nPress 'q' to quit.", m.err))
	}

	if !m.ready {
		return "Initializing terminal..."
	}

	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")

	if m.searchBar.Focused() {
		sb.WriteString(footerStyle.Render(m.searchBar.View()))
	} else {
		sb.WriteString(m.renderFooter())
	}

	return sb.String()
}

func (m *taskListModel) updateViewportContent() {
	rendered := m.renderTasks()
	m.viewport.SetContent(rendered)
}

func (m *taskListModel) updateViewportHeight() {
	if !m.ready {
		return
	}
	m.viewport.Height = m.height - m.calculateFooterHeight()
}

func (m *taskListModel) calculateFooterHeight() int {
	return 1
}

func (m *taskListModel) renderTasks() string {
	if len(m.tasks) == 0 {
		return "No tasks."
	}

	var sb strings.Builder
	for i, task := range m.tasks {
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}

		status := "[ ]"
		statusLabel := "pending"
		if task.Status == "completed" {
			status = "[x]"
			statusLabel = "completed"
		}

		line := fmt.Sprintf("%s%d. %s %s (%s)", prefix, i+1, status, task.Content, statusLabel)

		if i == m.selected {
			line = highlightStyle.Render(line)
		}

		sb.WriteString(line)
		if i < len(m.tasks)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func (m *taskListModel) renderFooter() string {
	start := m.pageOffset + 1
	end := m.pageOffset + len(m.tasks)
	if len(m.tasks) == 0 {
		start = 0
		end = 0
	}

	return footerStyle.Render(
		fmt.Sprintf("j/k: select • n/p: page • /: filter • q: back • showing %d-%d of %d", start, end, m.totalCount),
	)
}
