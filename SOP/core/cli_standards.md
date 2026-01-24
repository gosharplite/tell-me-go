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
```bash
alias a='tell-me-go'
```
This alias should be the primary way users interact with the tool, relying on the "Triple-Mode" logic above.

#### 4. Flag Parsing Location
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
if prompt == "" {
    stat, _ := os.Stdin.Stat()
    if (stat.Mode() & os.ModeCharDevice) == 0 {
        // Mode 2: Piped Stdin
        b, _ := io.ReadAll(os.Stdin)
        prompt = string(b)
    } else {
        // Mode 3: Interactive
        fmt.Println("Enter prompt (Ctrl+D to finish):")
        b, _ := io.ReadAll(os.Stdin)
        prompt = string(b)
    }
}
prompt = strings.TrimSpace(prompt)
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

