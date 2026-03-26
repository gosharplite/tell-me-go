# Interactive History Browser

**Status**: Released  
**Author**: Principal Software Architect

## Overview

The **Interactive History Browser** is a rich, keyboard-driven Terminal User Interface (TUI) for exploring your entire `tell-me-go` conversation history. It safely stitches your active memory and your archived disk history into a single, unbounded, scrollable view without risking Out-Of-Memory (OOM) crashes.

Unlike the static `-l N` flag, the browser offers full-text search, thought toggling, and the ability to pin or rollback messages directly from the UI.

## Command

To launch the browser for a specific session, use the new `browse` subcommand:

```bash
tell-me-go browse -c configs/assistant.yaml
```

*(Note: It accepts the exact same configuration flags as the `chat` command so it targets the correct history file).*

---

## Controls & Keybindings

Once the interface loads, you will see your history formatted with colors, tool execution logs, and an interactive footer.

### 1. Basic Navigation
- **`↑` / `k` (up arrow):** Scroll up line-by-line.
- **`↓` / `j` (down arrow):** Scroll down line-by-line.
- **`Page Up` / `Page Down`:** Scroll by full pages.
- **`Spacebar`:** Toggle the visibility of the AI's internal `<thinking>` processes (shown in gray text).
- **`q` or `Ctrl+C`:** Quit the browser and return to the shell.

*Note: Scrolling to the bottom will automatically load older history from the disk archive.*

### 2. Full-Text Search
- **`/` (forward slash):** Opens the search bar at the bottom of the screen.
- Type your query and press **`Enter`**.
- The UI will instantly highlight all case-insensitive matches in yellow.
- **`n`:** Jump to the **next** match.
- **`N` (Shift+n):** Jump to the **previous** match.
- **`Esc`:** Clear the search and remove highlights.

### 3. Direct History Manipulation
You can directly modify the AI's active memory from the browser. 

First, select a turn by pressing **`j`** or **`k`** until the `> ` cursor points to the message you want to modify.

- **`p` (Pin/Unpin):** Pins the selected turn. Pinned turns are marked with an orange `[PINNED]` badge and are protected from the AI's auto-summarization garbage collector. Press `p` again to unpin.
- **`r` (Rollback):** Destroys all history from the selected turn to the present. The UI will instantly refresh to reflect the deleted timeline.

#### The Archive Boundary
Older conversations are automatically moved to disk (`history.archive.jsonl`) to save memory. Messages in the browser marked with `(archived)` are strictly read-only. **You cannot Pin or Rollback archived messages.** If you try, the interface will ignore the command.

### 4. Live Reload
If you have the `tell-me-go browse` window open in one terminal pane, and you are actively chatting with `tell-me-go chat` in another pane, the browser will automatically detect file changes and live-update to show the AI's new responses in real-time.

---

## Warnings & Constraints

- **Pinning Pressure:** If you pin too many active messages, the AI will not have enough token budget left to auto-summarize the rest of the conversation, resulting in an eventual context-window crash. If you pin a large amount of text, a `⚠️ High Pinning Pressure` warning will appear in the footer.
- **TTY Requirement:** The `browse` command requires an interactive terminal. If you pipe the output (e.g., `tell-me-go browse | grep "foo"`), the application will gracefully abort. For scripting, continue to use `tell-me-go chat -l 50`.