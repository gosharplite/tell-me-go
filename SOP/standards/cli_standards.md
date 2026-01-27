// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT



# Standard Operating Procedure (SOP): CLI Command Line Interface Standards

### Objective
To ensure a consistent and user-friendly command-line interface for `tell-me-go`, defining how flags, arguments, and help text are managed.

---

### Prerequisites
- Go toolchain 1.24+.
- Standard `flag` package.

---

### Step-by-Step Instructions

#### 1. Flag Definitions
All flags must follow a consistent naming convention:
- **Config**: `-c` or `--config` for specifying the configuration YAML path.
- **Model**: `-m` or `--model` for overriding the AI model.
- **Verbose**: `-v` or `--verbose` for detailed logging.
- **History**: `-l` for showing conversation history. If used without a numeric argument (e.g., `b -l`), it defaults to showing the single last message (`b -l 1`).

#### 2. Argument and Input Handling
The tool follows a "Triple-Mode" input strategy to provide a seamless user experience:
- **Mode 1: Direct Argument**: If the first non-flag argument is present, use it as the prompt.
- **Mode 2: Piped Input**: If no argument is present and `stdin` is a pipe (not a terminal), read the prompt from `stdin`.
- **Mode 3: Interactive Multi-line**: If no argument is present and `stdin` is a terminal, enter an interactive mode that reads until `EOF` (`Ctrl+D`).

#### 3. Standard Alias (`b`)
To maintain consistency with the project's roots, users are encouraged to set up a single, universal alias:

#### 4. Session Management and Archiving
The tool manages session state through history and log files.
- **New Session**: When the `-new` flag is used, the tool must start a fresh conversation.
- **Automatic Archiving**: To prevent data loss, instead of deleting old session files, the tool **MUST** move them to a timestamped backup directory: `output/backups/YYYYMMDD_HHMMSS/`. 
- **Files to Archive**: This includes history (`_history.json`), token logs (`_tokens.log`), and command logs (`_commands.log`). 
- **Persistent Files**: Safety settings (`_safepaths.json`), scratchpads (`_scratchpad.md`), tasks (`_tasks.json`), and the bypass state (`_bypass.log`) are **not** archived to ensure persistent environment state and project context across sessions.
```bash
alias b='tell-me-go'
```
This alias should be the primary way users interact with the tool, relying on the "Triple-Mode" logic above.

#### 5. Latency and User Feedback (⚠️ CRITICAL)
To ensure a responsive user experience:
- **Immediate Echo**: The user's prompt must be echoed back (e.g., `> prompt`) immediately after input is finalized (e.g., after `Ctrl+D` or argument parsing).
- **Execution Order**: This echo **MUST** occur before any slow operations, such as configuration loading, network calls, or authentication (e.g., `gcloud auth`).

#### 6. Logging and Observability
All terminal log outputs (prompt echoes, system messages, thought processes, tool calls) must include a timestamp in the following format:
- `[HH:MM:SS]` (e.g., `[14:30:05]`)
- **System Status Indicator**: The primary payload log must show current resource usage relative to limits: `[System (CurrentTurn/MaxTurn)] Payload: ~Tokens tokens`.
- Colors:
    - User Prompt: Green (`[0;32m`)
    - System/Thought/Tool logs: Gray (`[0;90m`) to `stderr`.
    - Safety Warnings: Yellow or Red for urgency.

#### 7. Flag Parsing Location
- Flags should be defined and parsed within `cmd/tell-me-go/main.go`.
- Defaults should be sensible (e.g., default config path to `configs/vertex.yaml`).

---

### Code Templates

#### Standard Input and Flag Parsing:
```go
// 1. Parse Flags
configPath := flag.String("c", "configs/vertex.yaml", "Path to config")
flag.Parse()

// 2. Resolve Prompt (Triple-Mode)
prompt := flag.Arg(0)
// ... (input logic)
prompt = strings.TrimSpace(prompt)

// 3. Immediate User Feedback with Timestamp
fmt.Fprintf(os.Stderr, "[0;32m[%s] > %s[0m
", time.Now().Format("15:04:05"), prompt)

// 4. Proceed to Load Config (potentially slow)
cfg, err := config.Load(*configPath)
```

#### 9. Agentic State Management (⚠️ CRITICAL)
To prevent "Agentic Amnesia" and ensure the reliability of long-running or multi-turn tasks:

- **Atomic Turns**: Major technical actions (e.g., `git push`, `write_file`, `go test`) **MUST** be executed in their own turn. 
- **Immediate State Sync**: The turn immediately following a milestone action **MUST** be used to update the Task Manager (`manage_tasks`) and Scratchpad (`manage_scratchpad`). 
- **No Batching**: Do not batch technical milestones with administrative completion (e.g., don't perform a `git push` and then immediately say "I'm done" in the same response).
- **Mandatory Verification**: After updating the state, the agent **MUST** list the tasks or read the scratchpad to verify the write was successful and the state is consistent.
- **Tool Error Handling**: If a state-management tool (Task/Scratchpad) returns an error (e.g., "failed to parse"), this MUST be treated as a **blocking failure**. The agent must stop and fix the underlying file corruption before proceeding.

---

### Verification
1.  **Help Text**: Run `tell-me-go -h` to verify that all flags are documented.
2.  **Usage Errors**: Run the tool without arguments to verify the usage message.
3.  **Override Check**: Verify that passing `-c` actually changes the configuration being loaded.
4.  **State Consistency**: Verify that `manage_tasks list` matches the actual state of the project.

---

### Best Practices
- **Posix Compliance**: Support both short (`-c`) and long (`--config`) flags if using a third-party library, or stick to standard Go single-dash flags.
- **Exit Codes**: Use `os.Exit(1)` for user errors and `os.Exit(0)` for successful completion.
- **Standard Error**: Print error and usage messages to `os.Stderr`.


#### 8. Configuration File Templates
To ensure discoverability of features and ease of setup:
- **Completeness**: All configuration templates (e.g., `configs/vertex.yaml`) **MUST** include every variable supported by the application's `Config` struct.
- **Sensible Defaults**: Variables should be set to safe, sensible defaults.
- **Documentation**: Each variable in the template should be accompanied by a comment explaining its purpose, especially for safety limits like `MAX_HISTORY_TOKENS` or `MAX_TURNS`.
