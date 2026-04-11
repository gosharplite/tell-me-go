# ADR 002: Windows Compatibility Strategy

## Status
Accepted

## Context
Providing a first-class developer experience on Windows presented unique challenges. Many of the tools, particularly those in the `shellTool` suite, were originally designed with POSIX-centric assumptions (e.g., assuming `sh -c` is always available or using `ls` for file listing).

Furthermore, Windows handles file system locks and process cleanup differently, which led to transient errors (e.g., `The process cannot access the file because it is being used by another process`) and zombie processes during timeouts.

## Decision
We adopted a multi-layered approach to Windows compatibility:

1. **Strategy Pattern for Tooling**: We decoupled OS-specific command behaviors from the core `shellTool` using two strategy interfaces:
    * `commandTranslator`: Responsible for mapping POSIX-like commands (like `ls`, `rm -rf`) to their native Windows equivalents (`cmd /c dir`, `rd /s /q`).
    * `shellWrapper`: Encapsulates the logic for wrapping a command string in a shell executor. On Windows, it prefers `pwsh` or `powershell` if PowerShell-specific features (like cmdlets or `$env:`) are detected, falling back to `cmd.exe`.
2. **Transient Error Handling (`fsRetry`)**: A dedicated retry mechanism was implemented for file system operations on Windows. It catches specific error codes (like `0x20` or `ERROR_SHARING_VIOLATION`) and performs exponential backoff to handle transient locks by anti-malware scanners or indexing services.
3. **Hardened Process Cleanup**: The `processExecutor` was updated to ensure that entire process trees are terminated on Windows by using `taskkill /F /T /PID` when a context timeout occurs, preventing resource leaks.

## Consequences
- **Clean Core Architecture**: The `shellTool` remains a pure coordinator, agnostic of the host operating system.
- **Improved Extensibility**: New shells (like `zsh` on macOS) can be supported by simply implementing a new strategy.
- **Reliability**: File system operations and command execution are significantly more robust on Windows, with fewer intermittent failures.
- **Configuration Overhead**: OS-specific strategies must now be correctly instantiated and injected during the dependency injection phase in `registration.go`.
