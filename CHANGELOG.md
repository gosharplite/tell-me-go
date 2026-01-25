# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
## [0.8.1] - 2026-01-26

### Documentation
- **README Update**: Added documentation for `TELL_ME_HOME` and `AIT_HOME` environment variables, explaining how to share state and history with the original Bash project.
- **Feature Highlights**: Updated feature list to include new agentic tools (`execute_command`, `read_url`) and performance optimizations.

## [0.8.0] - 2026-01-26

### Added
- **State Management Tools**: Implemented `manage_scratchpad` and `manage_tasks` tools for persistent context.
- **Bash Compatibility**: Full compatibility with the original Bash project's data structure. 
- **TELL_ME_HOME Support**: Added support for `TELL_ME_HOME` and `AIT_HOME` environment variables to share history/tasks across projects.
- **Unified Naming**: Session files now match the Bash version exactly (e.g., `output/vertex.json`).

## [0.7.2] - 2026-01-26

### Changed
- **Auth Performance**: Implemented local token caching in `/tmp` to match the performance of the original Bash version. Reduces execution latency by ~500ms-1s on subsequent runs.

## [0.7.1] - 2026-01-26

### Changed
- **Tool UI Refinement**: Aligned `execute_command` and `ask_user` with the original Bash project style.
- **Single-Key Confirmation**: Users can now confirm command execution with a single key press ('y').
- **Auto-Approval**: Implemented a safety whitelist for read-only commands (ls, cat, grep, etc.) to skip confirmation.
- **Output Streaming**: Command output is now streamed in real-time to stderr with grey indentation.

## [0.7.0] - 2026-01-26

### Added
- **System Interaction Tools**: Added `execute_command` with a safety confirmation gate, allowing the model to run shell commands.
- **Interactive Clarification**: Added `ask_user` tool, allowing the agent to request information from the user during execution.
- **Web Content Retrieval**: Added `read_url` tool for fetching content from websites using `net/http`.

## [0.6.2] - 2026-01-26

### Fixed
- **Thinking Config Validation**: Fixed an issue where the Gemini API would return a 400 error if both `THINKING_BUDGET` and `THINKING_LEVEL` were sent. They are now mutually exclusive, with `THINKING_LEVEL` taking precedence.


## [0.6.1] - 2026-01-26

### Added
- **Timestamps**: Added `[HH:MM:SS]` timestamps to prompt echoes, model thinking, and tool calling logs for better observability.

### Changed
- **UI Performance**: Moved user prompt echo to occur immediately after input submission, eliminating the delay caused by `gcloud` authentication.

## [0.6.0] - 2026-01-26

### Added
- **Official SDK Migration**: Fully migrated to the `google.golang.org/genai` Go SDK.
- **Gemini 2.0 Thinking Support**: Added support for Reasoning/Thinking features.
- Configurable `THINKING_BUDGET` and `THINKING_LEVEL` (LOW, MEDIUM, HIGH, MINIMAL).
- Display of model thought processes in the terminal.
- **Search Retrieval**: Integrated native Google Search retrieval support (backend-aware).
- **System Instructions**: Support for `Person` config via the SDK's `SystemInstruction` field.

### Changed
- **Tool Orchestration**: Standardized on SDK-native function calling and role alternation.
- Improved `internal/api` with automatic project/location detection and simplified configuration.
- Updated `internal/history` to use SDK-native `Content` and `Part` types with strict role validation.
- Updated all SOPs to reflect SDK standards and Gemini 2.0 capabilities.

### Fixed
- Improved stability of multi-turn tool calling on Vertex AI by utilizing the official SDK's payload management.
## [0.2.1] - 2026-01-25

## [0.5.0] - 2026-01-25

### Removed
- **Deprecation**: Removed support for Google AI Studio (API Key authentication).
- Removed `APIKeyAuth` from `internal/auth`.
- Removed `API_KEY` environment variable and config field support.
- Deleted `configs/gemini.yaml`.

### Changed
- Focus exclusively on **Google Vertex AI**.
- Default configuration now points to `configs/vertex.yaml`.
- Simplified `internal/api` and `internal/auth` by removing query-parameter-based authentication logic.
- Updated all SOPs and README to reflect single-provider (Vertex AI) focus.

## [0.4.0] - 2026-01-25

### Added
- New `internal/agent` package: Refactored the orchestration loop into a testable package.
- Comprehensive Agent Tests: Added tests that simulate complex tool-calling scenarios, including `thought` and `thoughtSignature` preservation.

### Changed
- Improved Architecture: Moved business logic out of `cmd/tell-me-go/main.go` into `internal/agent` to comply with updated SOPs.
- Updated SOPs:
    - `SOP/core/architecture_and_packages.md`: Now mandates business logic resides in testable packages.
    - `SOP/core/testing_standards.md`: Now mandates mocking complex, multi-part API responses.
    - `SOP/core/agentic_capabilities.md`: Explicitly mandates preserving model thoughts and signatures to prevent Vertex AI 400 errors.

## [0.3.1] - 2026-01-25

### Fixed
- Vertex AI `thought_signature` Error: Updated `Part` struct to preserve model thoughts and signatures in history. This fixes the "missing a thought_signature" 400 error during tool calling.
- UI: Model thoughts are now displayed in gray on `stderr` to align with the original Bash project style.

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

