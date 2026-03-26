# ADR-009: TUI Interactive Prompt Mode with Auto-completion

## Status
Proposed (2026-02-26)

## Context
Currently, `tell-me-go` captures user input either as command-line arguments or via a simple "Press Ctrl+D to send" multi-line reader. While functional, it lacks modern CLI conveniences such as:
1. **Auto-completion**: No suggestions for frequently used prompts, tool names, or file paths.
2. **Visual Feedback**: The user has no real-time visibility into session state (token counts, turn limits) while composing their message.
3. **Interactive Navigation**: Navigating multiline input is cumbersome in a standard `io.ReadAll` loop.

We have already successfully integrated the Bubble Tea TUI framework for history browsing (`tell-me-go browse`), establishing a pattern for rich interactive terminal experiences.

## Decision
We will implement an **Interactive TUI Prompt Mode** using Bubble Tea as an **optional, opt-in alternative** to the current multi-line input.

### 1. Opt-In Mechanisms
To respect the "Keep-As-Is" requirement for the current Ctrl+D input mode:
- **Flag-based**: Users can run `tell-me-go -i` or `tell-me-go --tui` to explicitly launch the TUI prompt.
- **Config-based**: A new `USE_TUI_PROMPT` setting in `assistant.yaml` can be used to make the TUI the default for interactive sessions. **This defaults to `false` to preserve the current multi-line behavior.** If this setting is `true`, then and only then will running `tell-me-go` without arguments use the TUI instead of the Ctrl+D mode.

### 1. The Prompt TUI Model
We will create a new TUI model in `internal/ui/tui/prompt.go` that incorporates:
- **Textarea Component**: For multi-line message composition with familiar keybindings.
- **Auto-completion Engine**: A non-blocking suggestion system that provides real-time completions for:
    - **History**: Recent user messages.
    - **Tools**: Registered tool names.
    - **Files**: Local workspace file paths.
- **Session Dashboard**: A status bar or header showing:
    - Current Turn Count (e.g., `3/20`).
    - Token Usage (e.g., `4,500 / 180,000`).
    - Active Provider/Model.

### 2. Suggestion Architecture
To keep the TUI responsive, suggestions will be powered by a `SuggestionService` that pre-fetches and indexes relevant data:
- **HistorySuggestions**: Pulled from a new `GlobalPromptTracker` (`global_prompts.jsonl`) containing the top 1000 cross-session prompts. (Using JSON Lines for lock-free append-only writes).
- **PromptSuggestions**: Pre-defined prompts from `docs/user/prompts.md`.
- **FileSuggestions**: Dynamic `os.ReadDir` with intelligent caching (bounded and security-aware).

### 3. Integration with Capturer
The existing `ports.Capturer` interface will be extended or implemented by a `TUICapturer` that launches the Bubble Tea program and returns the final string once the user presses `Ctrl+S` or `Enter` (configurable).

## Consequences

### Positive
- **Superior UX**: Professional-grade input experience comparable to modern IDEs or advanced chat interfaces.
- **Reduced Friction**: Tab-completion for complex tool names or long file paths significantly speeds up developer workflows.
- **Context Awareness**: Real-time token/turn tracking prevents "blind" context exhaustion.

### Negative
- **Dependency Depth**: Further reliance on Bubble Tea/Lipgloss.
- **Complexity**: Managing multi-line input and auto-completion overlays in a terminal requires careful state management.

### Risks & Mitigations
- **Risk**: TUI input might be incompatible with some terminal environments or SSH sessions.
- **Mitigation**: Always maintain the existing "Simple Mode" (Ctrl+D) as a fallback when `TERM=dumb` or when not in a TTY.
- **Risk**: Blocking I/O during file system scanning for suggestions.
- **Mitigation**: Perform suggestion generation in asynchronous `tea.Cmd` goroutines.

## Technical Implementation Notes
- **Keybindings**: 
    - `Tab`: Cycle suggestions.
    - `Enter`: New line.
    - `Ctrl+S` or `Ctrl+Enter`: Submit prompt.
    - `Esc`: Cancel/Quit.

## Architectural Constraints (Post-Review)
Based on Principal Architect review, the following strict architectural constraints apply to this implementation:

### 1. [ARCHITECTURAL BLOCKER] Synchronous Autocomplete I/O & Goroutine Leaks
**Risk:** Triggering disk I/O (directory scanning for files) or complex history parsing on every single keystroke will block the Bubble Tea UI thread, causing severe input lag. Conversely, firing un-managed async requests will lead to goroutine leaks and CPU spikes.
**Constraint:** All auto-complete fetching MUST be executed asynchronously using Bubble Tea's `tea.Cmd`. Furthermore, the `SuggestionService` MUST accept a `context.Context` to correctly cancel previous operations upon subsequent keystrokes, combined with a 50-100ms debouncer. The `Update` function must never block on disk I/O or heavy computation.

### 2. [TECHNICAL DEBT] "God Object" TUI Model & Dependency Injection
**Risk:** Packing the Dashboard, Input, Auto-completion Engine, and Suggestion Dropdown into a single `PromptModel` struct violates the Single Responsibility Principle (SRP). If the TUI models instantiate their own services, it breaks Clean Architecture.
**Constraint:** The UI must be implemented using Component Composition. Specifically, separate the UI into specialized models (e.g., `MainTUIModel` containing `Dashboard`, `Prompt/Textarea`, and `Suggester/Autocomplete` components). The `SuggestionService` and `UnifiedHistoryProvider` MUST be injected from the application composition root.

### 3. [REFACTOR] CQRS for Real-Time Querying
**Risk:** The existing `UnifiedHistoryProvider` and `ToolRegistry` are designed for point-in-time reads and will degrade performance if scanned linearly on every keystroke.
**Constraint:** Implement an optimized Read Model (CQRS) specifically for the `SuggestionService`. Cache tool names, file paths, and recent history entries in a Radix Tree (Trie) or an in-memory inverted index upon TUI initialization to ensure `O(k)` prefix matching.

### 4. [TECHNICAL DEBT] Cross-Platform State Mutation
**Risk:** Managing concurrent state using `syscall.Flock` on a shared monolithic `.json` file for the global prompt tracker across disparate OS platforms (Windows vs POSIX) is notoriously brittle and a source of deadlocks or corruption.
**Constraint:** Implement an append-only JSON Lines (`global_prompts.jsonl`) design for tracking cross-session prompts. Utilize `os.O_APPEND` for atomic, lock-free sequential writes that offload synchronization to the operating system.

- **Component Layout**:
    ```text
    ╭─⠿ Turn 51
    [14:51:20] Payload: 53873/200000 tokens
    [14:51:20] [google] M: 2141 H: 51732 C: 242 Th: 0 ($0.0044) [3.82s (ΣT: 0.00s) / 48.43s]
    ╰─⠿ Ready ($0.0044 $0.0404 $0.3280 $22.5537 M: 423791 H: 1542792 78.5% O: 12989)

    [Coder prompts. Press Ctrl+S to send]

    ```
