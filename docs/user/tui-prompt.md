# Interactive TUI Prompt

The Interactive TUI (Text User Interface) Prompt is a modern, responsive alternative to the standard `Ctrl+D` multi-line input mode in `tell-me-go`. It is designed to provide a professional-grade experience with real-time auto-completion, session metrics, and a dedicated multi-line editor.

## Features

1. **Auto-completion Engine:** Get real-time suggestions as you type. The engine pulls from:
   - **Recent History:** Your past prompts across all sessions.
   - **File System:** Local workspace file paths (start typing `./` or `/`).
   - **Registered Tools:** Available system tools and commands.
2. **Session Dashboard:** View real-time token usage, turn counts, and the currently active provider/model at the top of the prompt.
3. **Multi-line Editing:** Safely paste and edit large blocks of text without breaking your terminal layout.

## How to Enable It

By default, `tell-me-go` uses the standard multi-line input to remain backward-compatible with scripts and basic environments. You can activate the TUI prompt in two ways:

### 1. Via Command Line Flag (Per Session)

To use the TUI prompt for a single command, append the `-i` or `--tui` flag:

```bash
tell-me-go -i
```

### 2. Via Configuration (Default for All Sessions)

If you prefer to always use the TUI prompt, you can set it as the default in your `assistant.yaml` or `config.yaml` file:

```yaml
# assistant.yaml
USE_TUI_PROMPT: true
```
*Note: Even if enabled in the config, the system will automatically fall back to the standard input if it detects a non-TTY environment (like a CI/CD pipeline or piped input).*

## Keybindings

When the TUI prompt is active, use the following keybindings to navigate and submit:

| Keybinding | Action |
| :--- | :--- |
| **`Tab`** | Cycle forward through the auto-complete suggestions. |
| **`Shift+Tab`** | Cycle backward through the auto-complete suggestions. |
| **`Enter`** | Start a new line within the text editor. |
| **`Ctrl+S`** or **`Alt+Enter`** | Submit the prompt to the LLM. |
| **`Esc`** | Abort the prompt and exit. |

## File Path Auto-completion

To trigger file path suggestions, simply start typing a path. For example:

- Type `./` to see files in the current directory.
- Type `/tmp/` to see files in the `/tmp` directory.

*Security Note: The suggestion engine automatically ignores noisy directories like `.git`, `node_modules`, and binary folders to keep your suggestions clean and prevent memory exhaustion.*