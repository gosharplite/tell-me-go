# ADR-009: TUI Interactive Prompt Mode with Auto-completion

## Status
Accepted / Implemented (2026-03-26)

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
    - **History**: Recent user messages and top global prompts.
    - **Files**: Local workspace file paths via dynamic chunked scanning.
    - *(Deferred)*: Registered tool names and pre-defined prompts.
- **Session Dashboard**: A status bar or header showing:
    - Current Turn Count (e.g., `3/20`).
    - Token Usage (e.g., `4,500 / 180,000`).
    - Active Provider/Model.

### 2. Suggestion Architecture
To keep the TUI responsive, suggestions will be powered by a `SuggestionService` that pre-fetches and indexes relevant data:
- **HistorySuggestions**: Pulled from a new `GlobalPromptTracker` (`global_prompts.jsonl`) containing the top 50 cross-session prompts, combined with the active session history. (Using JSON Lines for lock-free append-only writes).
- **FileSuggestions**: Dynamic `os.ReadDir` with intelligent, chunked directory scanning (`ReadDir(100)`) that frequently yields to context cancellation to prevent UI blocking.
- **Tools & PromptTemplates**: Deferred to a future iteration to focus on stabilizing core history and file workflows.

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

### 3. [REFACTOR] CQRS for Real-Time Querying (Updated)
**Risk:** The existing `UnifiedHistoryProvider` and `ToolRegistry` are designed for point-in-time reads and will degrade performance if scanned linearly on every keystroke.
**Constraint & Resolution:** While initially proposed to use a strict Radix Tree (Trie) for `O(k)` prefix matching, the final implementation opted for an in-memory array with **`O(N)` fuzzy subsequence matching** (`matcher.IsSubsequence`). Because the read model is explicitly capped at initialization (e.g., top 50 global prompts + recent session history), the `O(N)` scan combined with 100ms debouncing remains highly performant. This approach also provides superior UX by allowing users to skip characters (fuzzy matching), which a strict Trie prefix search does not support.

### 4. [TECHNICAL DEBT] Cross-Platform State Mutation
**Risk:** Managing concurrent state using `syscall.Flock` on a shared monolithic `.json` file for the global prompt tracker across disparate OS platforms (Windows vs POSIX) is notoriously brittle and a source of deadlocks or corruption.
**Constraint:** Implement an append-only JSON Lines (`global_prompts.jsonl`) design for tracking cross-session prompts. Utilize `os.O_APPEND` for atomic, lock-free sequential writes that offload synchronization to the operating system.

### 5. Implementation Notes & Final Reality (Post-Implementation)
During implementation, several refinements were made to the original design to improve stability and UX:
- **Fuzzy Matching over Prefix Matching:** Opted for `matcher.IsSubsequence` over a Trie, as it scales adequately for a bounded context (top 50 history entries) and provides a more forgiving user experience for typos or abbreviations.
- **Chunked File Scanning:** File completion uses a chunked `ReadDir(100)` loop that yields to `context.Context` cancellation on every iteration. This guarantees rapid exit when a user continues typing, avoiding blocking the background thread during massive directory scans.
- **Deferred Sources:** Tool names and predefined templates were temporarily deferred to focus on delivering a stable core experience around history and file system navigation.
- **No Internal Dashboard:** The proposed internal dashboard (`Turn 3/20 | Tokens 4500`) was removed. Instead, the `tea.WithAltScreen()` constraint was dropped so the TUI renders inline. This allows the user to simply read the detailed, standard `╰─⠿ Ready` payload metrics from the previous turn directly above their text box, eliminating redundant clutter.
- **Debouncing:** A 100ms `tea.Tick` debouncer was introduced to prevent rapid keystrokes from spamming the system with `tea.Cmd` context cancellations and async requests.
- **Lock-Free Append:** Instead of using error-prone `syscall.Flock` for global history, the system strictly utilizes an append-only `.jsonl` structure via `os.O_APPEND` to ensure atomic state mutation across simultaneous terminal sessions.
