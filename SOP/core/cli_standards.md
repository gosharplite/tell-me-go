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

#### 2. Argument Handling
- **Prompt**: The primary prompt should be the first non-flag argument.
- **Validation**: The tool must provide a clear usage message if required arguments or flags are missing.

#### 3. Flag Parsing Location
- Flags should be defined and parsed within `cmd/tell-me-go/main.go` or a dedicated `internal/cli` package for complex commands.
- Defaults should be sensible (e.g., default config path to `configs/gemini.yaml`).

---

### Code Templates

#### Standard Flag Parsing:
```go
var configPath string
flag.StringVar(&configPath, "c", "configs/gemini.yaml", "Path to the configuration file")
flag.Parse()

prompt := flag.Arg(0)
if prompt == "" {
    fmt.Println("Usage: tell-me-go [flags] <prompt>")
    flag.PrintDefaults()
    os.Exit(1)
}
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

