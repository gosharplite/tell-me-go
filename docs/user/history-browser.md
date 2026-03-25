# Interactive History Browser

**Status**: Proposed Feature  
**Target Release**: Future Release  
**Author**: Principal Software Architect

## Overview

The **Interactive History Browser** is a proposed new feature for `tell-me-go` that provides a rich, keyboard-driven interface for exploring conversation history. Unlike the current `-l N` flag which produces static text output, the browser offers search, navigation, and interactive filtering of AI thoughts and tool calls.

## Motivation

When working with complex AI conversations, developers need to:
- Review the AI's reasoning process (internal "thoughts")
- Navigate long conversation histories efficiently  
- Search for specific content across turns
- Understand tool execution flows
- Debug complex multi-turn interactions

The current `-l N` output is insufficient for these use cases, especially with long conversations where AI thoughts are hidden by default.

## Proposed Implementation

### Command
```bash
tell-me-go browse [options]
```

### Options
- `-c, --config PATH`: Configuration file (default: `configs/assistant.yaml`)
- `--search QUERY`: Pre-populate search with query
- `--show-thoughts`: Start with thoughts visible (overrides config)
- `--export FILE`: Export selected turns to file (future)

### Key Features

#### 1. **Interactive Navigation**
```
[USER] What's the capital of France?
[MODEL] <thinking>I need to recall European geography...</thinking>
       Paris is the capital of France.
───────────────────────────────────────
↑/↓: Navigate  Space: Toggle thoughts  
/: Search     n/N: Next/prev match    q: Quit
```

#### 2. **Thought Visibility Toggle**
- Thoughts (`IsThought: true`) are shown/collapsed with spacebar
- Respects `SHOW_THOUGHTS` configuration but allows override
- Visual distinction: thoughts in gray, regular text in default color

#### 3. **Full-Text Search**
- `/` enters search mode
- Highlights matches across entire history
- `n`/`N` jump between matches
- Case-insensitive by default

#### 4. **Turn Navigation**
- Jump to specific turn number
- Expand/collapse tool call details
- View raw JSON for debugging

#### 5. **Direct History Manipulation** (Future)
- Pin/unpin turns from TUI
- Rollback turns directly
- Export selected conversations

## Technical Details

### Architecture
The browser builds on existing `tell-me-go` components:

1. **History Data**: Uses `HistoryManager.GetWindow()` to retrieve complete history with thoughts
2. **Configuration**: Respects existing `SHOW_THOUGHTS` YAML setting
3. **UI Framework**: Bubble Tea TUI (Charm ecosystem, already used for markdown rendering)
4. **CLI Integration**: New `browse` subcommand following existing patterns

### Dependencies
- **bubbletea**: TUI framework (+1 main dependency)
- **bubbles**: Reusable components (+1 main dependency)
- **Existing**: charmbracelet/glamour, charmbracelet/lipgloss (already present)

### Performance Considerations
- **Large Histories**: Lazy loading via `GetWindow()` pagination
- **Memory**: Only loads visible portion of history
- **Fallback**: Automatically uses text rendering when not in TTY

## Usage Examples

### Basic Exploration
```bash
# Launch interactive browser
tell-me-go browse -c configs/assistant.yaml

# Navigate with ↑/↓, toggle thoughts with space
# Search with /, quit with q
```

### Search-First Workflow
```bash
# Search for refactoring discussions
tell-me-go browse --search "refactor"

# Search with specific config
tell-me-go browse -c configs/code-review.yaml --search "security"
```

### Non-Interactive Use (Pipes/Automation)
```bash
# Fallback to text output for scripting
tell-me-go browse -l 50 | grep "ERROR"

# Export to file (future)
tell-me-go browse --export conversation.md
```

## Configuration Integration

### YAML Configuration
```yaml
# configs/assistant.yaml
SHOW_THOUGHTS: false  # Browser respects this default
SHOW_TOOLS: true      # Affects tool call display
```

### TUI Preferences (Future)
```yaml
# ~/.tell-me-go/tui.yaml (proposed)
browser:
  show_thoughts: true      # User preference overrides config
  color_scheme: "dark"
  keybindings:
    toggle_thoughts: " "
    search: "/"
    quit: "q"
```

## Comparison with `-l N`

| Feature | `-l N` (Current) | `browse` (Proposed) |
|---------|-----------------|-------------------|
| **Interactivity** | None | Full keyboard navigation |
| **Thought Visibility** | Config-only | Interactive toggle |
| **Search** | None | Full-text search |
| **Large History** | Overwhelming | Paginated, scrollable |
| **Tool Inspection** | Limited | Expandable details |
| **TTY Required** | No | Yes (with fallback) |
| **Scripting** | Excellent | Fallback mode |

## Development Roadmap

### Phase 1 (MVP)
- Basic scrolling navigation
- Thought visibility toggle  
- TTY detection and fallback
- Color-coded role display

### Phase 2
- Full-text search
- Turn jumping
- Tool call expansion
- Help screen

### Phase 3  
- Direct pin/unpin
- Rollback interface
- Multi-session support
- Export functionality

## Technical Constraints

1. **TTY Requirement**: Bubble Tea requires interactive terminal
   - **Solution**: Automatic fallback to text rendering
2. **Binary Size**: +2MB from dependencies
   - **Impact**: 25MB → 27MB total
3. **Learning Curve**: New keybindings
   - **Mitigation**: On-screen help, intuitive defaults

## Related Documentation

- [ADR-008: Bubble Tea Interactive History Browser](../adr/2026-02-bubble-tea-history-browser.md)
- [History Management SOP](../sop/technical/history_management.md)
- [UI Rendering Architecture](../adr/2024-10-history-log-compaction.md)

## Feedback & Contribution

This feature is currently in proposal phase. Feedback can be provided through:
- GitHub Issues
- Architecture review discussions
- User experience testing

The implementation will follow the project's clean architecture patterns and existing code standards.