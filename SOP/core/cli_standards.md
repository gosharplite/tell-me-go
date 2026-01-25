# Copyright (c) 2026 gosharplite@gmail.com
# SPDX-License-Identifier: MIT

# Standard Operating Procedure (SOP): CLI Command Line Interface Standards

### Objective
To ensure a consistent and user-friendly command-line interface for `tell-me-go`, defining how flags, arguments, and help text are managed.

---

### Prerequisites
- Go toolchain 1.21+.
- Standard `flag` package.

---

### Step-by-Step Instructions

#### 1. Flag Definitions
All flags must follow a consistent naming convention:
- **Config**: `-c` or `--config` for specifying the configuration YAML path.
- **Model**: `-m` or `--model` for overriding the AI model.
- **Verbose**: `-v` or `--verbose` for detailed logging.

#### 2. Argument and Input Handling
The tool follows a "Triple-Mode" input strategy to provide a seamless user experience:
- **Mode 1: Direct Argument**: If the first non-flag argument is present, use it as the prompt.
- **Mode 2: Piped Input**: If no argument is present and `stdin` is a pipe (not a terminal), read the prompt from `stdin`.
- **Mode 3: Interactive Multi-line**: If no argument is present and `stdin` is a terminal, enter an interactive mode that reads until `EOF` (`Ctrl+D`).

#### 3. Standard Alias (`a`)
To maintain consistency with the project's roots, users are encouraged to set up a single, universal alias:

#### 4. Session Management and Archiving
The tool manages session state through history and log files.
- **New Session**: When the `-new` flag is used, the tool must start a fresh conversation.
- **Automatic Archiving**: To prevent data loss, instead of deleting old session files, the tool **MUST** move them to a timestamped backup directory: `output/backups/YYYYMMDD_HHMMSS/`. 
- **Files to Archive**: This include history (`.json`), logs (`.log`), scratchpads (`.scratchpad.md`), and tasks (`.tasks.json`).
```bash
alias a='tell-me-go'
```
This alias should be the primary way users interact with the tool, relying on the "Triple-Mode" logic above.

#### 5. Latency and User Feedback (⚠️ CRITICAL)
To ensure a responsive user experience:
- **Immediate Echo**: The user's prompt must be echoed back (e.g., `> prompt`) immediately after input is finalized (e.g., after `Ctrl+D` or argument parsing).
- **Execution Order**: This echo **MUST** occur before any slow operations, such as configuration loading, network calls, or authentication (e.g., `gcloud auth`).

#### 6. Logging and Observability
All terminal log outputs (prompt echoes, system messages, thought processes, tool calls) must include a timestamp in the following format:
- `[HH:MM:SS]` (e.g., `[14:30:05]`)
- Colors:
    - User Prompt: Green (`\033[0;32m`)
    - System/Thought/Tool logs: Gray (`\033[0;90m`) to `stderr`.

#### 7. Flag Parsing Location
- Flags should be defined and parsed within `cmd/tell-me-go/main.go`.
- Defaults should be sensible (e.g., default config path to `configs/gemini.yaml`).

---

### Code Templates

#### Standard Input and Flag Parsing:
```go
// 1. Parse Flags
configPath := flag.String("c", "configs/gemini.yaml", "Path to config")
flag.Parse()

// 2. Resolve Prompt (Triple-Mode)
prompt := flag.Arg(0)
// ... (input logic)
prompt = strings.TrimSpace(prompt)

// 3. Immediate User Feedback with Timestamp
fmt.Fprintf(os.Stderr, "\033[0;32m[%s] > %s\033[0m\n", time.Now().Format("15:04:05"), prompt)

// 4. Proceed to Load Config (potentially slow)
cfg, err := config.Load(*configPath)
```

---

### Verification
1.  **Help Text**: Run `tell-me-go -h` to verify that all flags are documented.
2.  **Usage Errors**: Run the tool without arguments to verify the usage message.
3.  **Override Check**: Verify that passing `-c` actually changes the configuration being loaded.

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

