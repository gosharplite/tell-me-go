# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.1] - 2026-01-25

## [0.3.0] - 2026-01-25

### Added
- Agentic Capabilities (Function Calling): The model can now call local tools to perform tasks.
- Multi-turn Tool Loop: Support for recursive model-tool interactions.
- File System Tools:
    - `list_files`: List directory contents.
    - `read_file`: Read file content.
- `internal/tools` package for tool registration and management.
- New `AddContent` method in `internal/history` for multi-part message support.

### Changed
- Refactored `internal/api` to support Gemini's `tools` and `functionCall`/`functionResponse` schemas.
- Updated the main CLI loop to handle tool execution orchestration.

## [0.2.4] - 2026-01-25

### Changed
- UI Cleanup: Removed the redundant "[System] Using ..." authentication message to reduce terminal noise.

## [0.2.3] - 2026-01-25

### Changed
- UI Refinement: Aligned system messages and prompt echoes with the original `tell-me` Bash style.
- System messages are now printed in gray (`\033[0;90m`) and sent to `stderr`.
- User prompt echoes are now printed in green (`\033[0;32m`) and sent to `stderr`.

## [0.2.2] - 2026-01-25

### Changed
- Simplified Shell Integration: Merged `aa` functionality into a single, smart `a` alias.
- Updated README to reflect the triple-mode behavior of the `a` alias.

### Added
- Interactive multi-line input support when no prompt is provided (similar to `aa` in Bash).
- Piped stdin support for analyzing files (e.g., `cat file | tell-me-go "Explain"`).
- Shell alias instructions (`a` and `aa`) in README.md.

### Fixed
- Updated `.gitignore` to prevent ignoring the `cmd/` directory.

## [0.2.0] - 2026-01-25
### Added
- Initial port from Bash to Go.
- Dual Auth: AI Studio (API Key) & Vertex AI (GCP Tokens).
- Session Persistence (History Management).
- SOP-driven architecture.

