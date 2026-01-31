<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.64.0] - 2026-01-31
### Added
- **Testing**: Added unit tests for `execute_command` and `pipe_commands` in `internal/tools/system_release_test.go`, significantly bridging the coverage gap for system-level tools.
### Fixed
- **Testing**: Resolved a race condition in `internal/history/history_test.go` by replacing hardcoded file paths with `t.TempDir()`, ensuring stable parallel test execution.
### Changed
- **Maintenance**: Promoted to v1.64.0 following the refined public release procedure.

## [1.63.0] - 2026-01-30
### Added
- **CLI**: The application now automatically seeds the mode-scoped `config.json` with core limits (`MAX_TURNS`, `MAX_HISTORY_TURNS`, `MAX_HISTORY_TOKENS`) if it doesn't exist. This improves discoverability of configurable parameters for new users.
- **Agent**: Implemented hot-reloading for the main YAML configuration. The agent now re-parses the configuration file at the start of every request, allowing for dynamic adjustments to limits and features without restarting the session.

### Changed
- **Refactor**: Restored explicit error logging during configuration parsing and simplified the internal configuration loading logic for better maintainability.
- **Maintenance**: Promoted to v1.63.0 following the refined public release procedure.

## [1.62.0] - 2026-01-30
### Fixed
- **Testing**: Updated `mockChatter` in `internal/cli/cli_test.go` to implement the full `agent.Chatter` interface, resolving a compilation error during release verification.

### Changed
- **Maintenance**: Promoted to v1.62.0 following the refined public release procedure.

## [1.61.0] - 2026-01-30
### Changed
- **Performance**: Implemented AST caching for intelligence tools. This significantly reduces file parsing overhead when running multiple AST-based queries (e.g., `find_usages`, `get_type_info`) in the same session.
- **Refactor**: Integrated `fsutil.AtomicWrite` into `history.ReplaceRange` to ensure data integrity during history updates.
- **Maintenance**: Promoted to v1.61.0 following the refined public release procedure.

### Fixed
- **Refactoring Tools**: Fixed import handling in `move_definition` using `golang.org/x/tools/imports` to ensure moved code compiles correctly.
- **Agent Stability**: Fixed a state synchronization issue by refreshing limits immediately after tool execution.

## [1.60.0] - 2026-01-30
### Added
- **Refactoring Tools**: Introduced `move_definition` for safe, AST-based movement of Go symbols (structs, interfaces, functions) and their associated methods between files.
- **Dynamic Pricing**: Updated the `estimate_cost` tool and local pricing engine to support tiered input pricing (Standard vs. Premium at 128k tokens) for Gemini models.

### Changed
- **Tool Clarity**: Performed a comprehensive refinement of tool descriptions across the `filesystem`, `intelligence`, `git`, `dev`, and `system` toolsets. This distinguishes between AST-based (precise) and Regex/Text-based (broad) search mechanisms to prevent AI misinterpretation.
- **Git Integration**: Optimized `git_commit` and `git_create_branch` descriptions to emphasize their roles in the development workflow.
- **Version Stabilization**: Finalized release v1.60.0.

## [1.59.0] - 2026-01-30

### Fixed
- **Testing**: Removed unused `msg` variable in `internal/tools/backup_test.go` to resolve a linter warning (SA4006).

### Changed
- **Maintenance**: Promoted to v1.59.0 following the refined public release procedure.

## [1.58.0] - 2026-01-30

### Added
- **Observability**: Implemented Task Execution Latency tracking. The metrics log now displays `AI_Time+Tool_Time` (e.g., `10.96s+7s`) if tool execution takes longer than 3 seconds, providing better visibility into long-running tasks.

### Changed
- **Maintenance**: Promoted to v1.58.0 following the refined public release procedure.

## [1.57.0] - 2026-01-30

### Changed
- **Maintenance**: Promoted to v1.57.0 following the refined public release procedure.

## [1.56.0] - 2026-01-30

### Changed
- **Maintenance**: Promoted to v1.56.0 following the refined public release procedure.

## [1.55.0] - 2026-01-30

### Fixed
- **CLI**: Ensured the application exits immediately after displaying history with the `-l` flag, preventing unintended prompt processing.
- **CLI**: Improved argument sanitization for history display and raw output flags.

### Changed
- **Maintenance**: Promoted to v1.55.0 following the refined public release procedure.

## [1.54.0] - 2026-01-30

### Added
- **CLI**: Restructured output directory to use mode-specific subdirectories (e.g., `output/vertex/`) for better isolation and organization.

### Fixed
- **Security**: Updated git safety logic in `execute_command` to correctly identify subcommands by skipping leading flags, improving the reliability of the read-only command whitelist.

### Changed
- **Maintenance**: Promoted to v1.54.0 following the refined public release procedure.

## [1.53.0] - 2026-01-30

### Added
- **Observability**: Added total session duration to the post-turn metrics log for better performance tracking.

### Changed
- **UX**: Refined tool descriptions to be more neutral and concise, minimizing instructional bias and preventing potential system prompt leaks.
- **Maintenance**: Promoted to v1.53.0 following the refined public release procedure.

## [1.52.0] - 2026-01-30

### Changed
- **Architecture**: Refactored `internal/cli` to improve testability and added comprehensive unit tests for the CLI package.
- **Maintenance**: Promoted to v1.52.0 following the refined public release procedure.

## [1.51.0] - 2026-01-30

### Added
- **Logging**: Implemented a two-phase logging approach for turn status. Pre-call logs show estimated payload and turn count, while post-call logs show actual metrics and final token counts.
- **UI**: Added cyan timestamps to `[Tool Engine]` logs for better visual grouping with tool prefixes.
- **Safety**: Enabled auto-approval for read-only `git` commands (status, diff, log, commit, blame) in `execute_command` to streamline developer workflows.
- **Stability**: Hardened the turn counter to remain stable during complex tool execution loops.
- **Testing**: Added regression tests for the refactored logging system in `internal/agent`.

### Changed
- **Maintenance**: Promoted to v1.51.0 following the refined public release procedure.

## [1.50.0] - 2026-01-30

### Added
- **History**: Implemented incremental history saving. The agent now persists history to disk after every significant state change (user prompt, model response, tool results).
- **History**: Added a `Repair()` mechanism that automatically detects and closes "dangling" tool calls after a system crash or reboot, ensuring the history remains valid for the Gemini API.

### Changed
- **Agent**: Refined history persistence to log warnings on failure instead of silently suppressing them.
- **Maintenance**: Promoted to v1.50.0 following the refined public release procedure.

## [1.49.0] - 2026-01-30

### Added
- **CLI**: Added `-r` flag for raw text output. This allows displaying history and chat responses without Markdown rendering, improving compatibility with plain-text environments and scripts.
- **Agent**: Implemented `SetRawOutput` to toggle between `glamour` Markdown rendering and plain text output.

### Changed
- **Documentation**: Updated `README.md` to document the new `-r` flag and provided examples for raw history and prompt output.
- **Maintenance**: Promoted to v1.49.0 following the refined public release procedure.

## [1.48.0] - 2026-01-30

### Changed
- **Refactor**: Re-engineered the tool registry to encapsulate tool execution behavior (serial vs. parallel) within tool definitions, fully adhering to the Open-Closed Principle (OCP).
- **Architecture**: Decoupled the `Agent` from specific tool names by utilizing a centralized `IsSerial(name)` registry query.
- **Safety**: Expanded mandatory serial execution to 18 mutation, interactive, and high-stakes tools (Filesystem, Git, State, Media, Teams, etc.) to ensure predictable terminal output and execution state.
- **Maintenance**: Address code review feedback by standardizing unexported map names and improving documentation consistency in the tools package.
- **Testing**: Added `internal/tools/tools_test.go` for comprehensive verification of tool registration and property logic.
- **Version Bump**: Promoted to v1.48.0.

## [1.47.0] - 2026-01-30

### Changed
- **Documentation**: Updated Public Release SOP with mandatory workspace integrity checks and sequential task initialization to prevent process amnesia.
- **Maintenance**: Performed comprehensive public release audit and aligned Task Manager IDs with SOP step numbers.
- **Version Bump**: Promoted to v1.47.0.

## [1.46.0] - 2026-01-28

### Added
- **Financial Metrics**: Implemented unique session IDs for cost recording. Archived sessions are now keyed by a unique ID (e.g., `backup/timestamp/logname`) in `global_costs.json` to prevent them from being overwritten by new sessions.
- **Testing**: Added a comprehensive End-to-End (E2E) test (`tests/e2e/archive_cost_test.go`) to verify cost preservation during session archiving.

### Fixed
- **CLI Initialization**: Fixed initialization order to ensure safe paths are registered before session archiving and cost recording occurs. This ensures archived sessions are correctly recorded in `global_costs.json`.
- **Atomic Writes**: Hardened `AtomicWrite` utility to use `fsync` and explicitly set file permissions, preventing stale reads and potential race conditions in state management.
- **Resource Cleanup**: Implemented automatic cleanup of temporary files in `AtomicWrite` on failure.

### Changed
- **Version Bump**: Promoted to v1.46.0.

## [1.45.0] - 2026-01-28
### Changed
- **Maintenance**: Promoted to v1.45.0 following the refined public release procedure.

## [1.44.0] - 2026-01-28
### Changed
- **Maintenance**: Promoted to v1.44.0 following the refined public release procedure.

## [1.43.0] - 2026-01-28
### Added
- **Financial Metrics**: Implemented lifecycle-based cost recording. The application now automatically calculates and appends the session cost to the daily ledger (`output/global_costs.json`) upon termination.

### Changed
- **Refactor**: Optimized metrics tools to support centralized, automated session cost recording.
- **Version Bump**: Promoted to v1.43.0.

## [1.42.0] - 2026-01-28

### Fixed
- **UI/UX**: Hardened terminal output synchronization using a global `TerminalMutex`. This prevents interleaved logs from concurrent tools from corrupting interactive prompts (e.g., `ask_user`).

### Changed
- **Version Bump**: Promoted to v1.42.0.

## [1.41.0] - 2026-01-28

### Added
- **UX Preference**: Added `configure_ux_preferences` tool to enable "Smart Suggestions" at the end of every response. This state is persistent and mode-scoped.
- **System Prompt Injection**: Implemented automatic injection of UX preferences into the AI's core instructions at session startup.
- **Observability**: Added warning logs for persistent configuration loading errors in the CLI.

### Changed
- **CLI Shell**: Optimized the `hack.sh` script to support dynamic command selection (`bbb` vs `ccc`) and retired it in favor of the new agentic Smart Suggestions.
- **Documentation**: Fully revised `README.md` to document the new UX customization capabilities.
- **Version Bump**: Promoted to v1.41.0.

## [1.40.0] - 2026-01-28

### Changed
- **Documentation**: Updated all Markdown files (README and SOPs) to use HTML comments for license headers, ensuring they are hidden in rendered output while remaining machine-readable.
- **Maintenance**: Performed comprehensive public release audit and documentation synchronization.
- **Version Bump**: Promoted to v1.40.0.

## [1.39.0] - 2026-01-28

### Added
- **Testing**: Added comprehensive unit tests for the AST-based `renameSymbol` refactor, verifying semantic accuracy and literal protection.

### Changed
- **Feat**: Implemented dynamic thinking budgets. The application now live-syncs reasoning limits from `assets/pricing.json` instead of using hardcoded values.
- **Refactor**: Upgraded `renameSymbol` to use Go's native AST (`go/ast`) and parser (`go/parser`). The tool now performs semantic renaming and automatically formats modified files using `go/format`.
- **Maintenance**: Stabilized the pricing engine by pointing the remote pricing URL to the `main` branch for production reliability.

## [1.38.0] - 2026-01-28

### Changed
- **Refactor**: Hardened project-wide thread-safety and implemented atomic writes for all state management tools (`manage_tasks`, `manage_scratchpad`). This ensures file integrity and prevents race conditions during parallel tool execution.
- **Maintenance**: Refactored `internal/tools` to use a centralized `AtomicWrite` utility, reducing code duplication and improving reliability.

## [1.37.0] - 2026-01-28

### Added
- **SOP**: Added "Thread Safety" and "Atomic Write" requirements to the `Agentic Capabilities` SOP.

### Fixed
- **Stability**: Implemented `sync.Mutex` and robust JSON parsing for state management tools to prevent data corruption and race conditions.
- **Refinement**: Improved state management logic based on code review for better robustness and clarity.

## [1.36.0] - 2026-01-28

### Added
- **SOP Hardening**: Integrated "Agentic State Management" standards into all project SOPs.
- **Standards**: Mandated **Atomic Turns**, **Immediate State Sync**, and **Mandatory Verification** in `SOP/standards/cli_standards.md` to prevent agentic amnesia.
- **Lifecycle**: Integrated state initialization requirements into `sop_management.md` and `self_update_safety.md`.

### Changed
- **Documentation**: Standardized "AI Agent Implementation Guides" across all relevant SOPs.
- **Release Process**: Promoted to v1.36.0 following the hardened public release procedure.

## [1.35.0] - 2026-01-28

### Added
- **API Error Handling**: Improved error reporting for empty API responses. The tool now identifies and reports specific block reasons (e.g., `SAFETY`, `RECITATION`) and `FinishMessage` context instead of a generic "empty response" error.
- **SOP Hardening**: Mandated "Anchor Tasking" and Scratchpad initialization in the `public_release.md` SOP to prevent process amnesia across sessions.

### Changed
- **Documentation**: Updated `README.md` to reflect descriptive error handling and path-based safety guardrails.
- **Maintenance**: Performed comprehensive security audit and clean-room build verification.
- **Version Bump**: Promoted to v1.35.0.

## [1.34.0] - 2026-01-28

### Fixed
- **State Persistence**: Fixed `safepaths`, `scratchpad`, and `tasks` persistence across new sessions (`-new`). These environment-level states now remain stable while conversation history is archived.
- **Safety**: Ensured `bypass_confirmation` state is preserved across sessions within the same mode.

### Added
- **Testing**: Added E2E tests for environment persistence and bypass state preservation to ensure stable multi-session workflows.

### Changed
- **Documentation**: Updated `README.md` to clarify the distinction between archived session history and persistent environment state.
- **Maintenance**: Performed comprehensive security audit and clean-room build verification.
- **Version Bump**: Promoted to v1.34.0.

## [1.33.0] - 2026-01-27

### Added
- **Safety**: Preserved the `bypass_confirmation` state across new sessions (`-new`). This ensures that automation approvals persist across tasks within the same mode.
- **Testing**: Added E2E verification for bypass state preservation.

### Changed
- **Documentation**: Updated Standard Operating Procedures (`SOP/standards/cli_standards.md` and `SOP/technical/security_and_sandbox.md`) to reflect the persistent nature of the bypass state.
- **Maintenance**: Performed a full security audit and clean-room build verification for public release.
- **Version Bump**: Promoted to v1.33.0.

## [1.32.0] - 2026-01-27

### Changed
- **Documentation**: Synchronized all Standard Operating Procedures (SOPs), README.md, and system configurations with the actual Go implementation.
- **Naming Convention**: Enforced a consistent underscore (`_`) naming convention for all session-persistent files across the codebase and documentation.
- **Compliance**: Added missing SPDX-License-Identifier headers to all project source files, including test files and SOPs.
- **Maintenance**: Performed a full security audit and clean-room build verification for public release.

## [1.31.0] - 2026-01-27

### Changed
- **Config**: Updated the naming convention for persistent session files to `<MODE>_history.json`, `<MODE>_tokens.log`, `<MODE>_commands.log`, `<MODE>_safepaths.json`, `<MODE>_bypass.log`, `<MODE>_tasks.json`, and `<MODE>_scratchpad.md` to improve clarity and organization.
- **Documentation**: Updated `SOP/technical/history_management.md` and `.gitignore` to reflect the new file naming standards.

## [1.30.0] - 2026-01-27

### Added
- **Testing**: Added unit and E2E tests for `global_costs.json` and metrics logic.
- **Safety**: Implemented file locking and corruption recovery for the cost ledger to prevent data loss during concurrent access.

### Changed
- **Architecture**: Scoped `manage_tasks` and `manage_scratchpad` to the configuration `MODE` (e.g., `tasks_vertex.json`).
- **Safety**: Enabled persistent `bypass_confirmation` across sessions.
- **Maintenance**: Performed comprehensive public release audit.
- **Version Bump**: Promoted to v1.30.0.

## [1.29.0] - 2026-01-27

### Added
- **Testing**: Added comprehensive unit tests for `manage_tasks` and `manage_scratchpad` tools.
- **Testing**: Integrated E2E tests for task management and scratchpad workflows, ensuring end-to-end tool orchestration stability.

### Changed
- **Architecture**: Modularized Agent, CLI, and Tools packages for better separation of concerns and testability.
- **Safety**: Updated system notices and safety warnings to include explicit instructions for Task List management.
- **State Management**: Refactored `manageTasks` with improved error handling for file operations and JSON parsing.
- **Maintenance**: Performed comprehensive public release audit, including security scanning, SPDX compliance verification, and clean-room build validation.

## [1.28.0] - 2026-01-27

### Changed
- **Maintenance**: Performed comprehensive public release audit.
- **Version Bump**: Promoted to v1.28.0.

## [1.27.0] - 2026-01-27

### Changed
- **Safety**: Enabled persistent `bypass_confirmation` across sessions.
- **UI/UX**: Hardened thinking budget calculation to avoid API errors.
- **Maintenance**: Performed comprehensive public release audit.
- **Version Bump**: Promoted to v1.27.0.

## [1.26.0] - 2026-01-27

### Changed
- **Safety**: Hardened `IsPathSafe` verification. Improved test robustness for path traversal detection when running the project from temporary or deep subdirectories.
- **Maintenance**: Performed comprehensive public release audit, including security scanning, SPDX compliance verification, and clean-room build validation.
- **Version Bump**: Promoted to v1.26.0.

## [1.25.0] - 2026-01-27

### Changed
- **Documentation**: Fully revised all Standard Operating Procedures (SOPs) and README.md for the public release.
- **Maintenance**: Performed a comprehensive security audit and clean-room build verification.
- **Version Bump**: Promoted to v1.25.0.

## [1.24.0] - 2026-01-27

### Changed
- **Documentation**: Synchronized `SOP/technical/architecture_and_packages.md` with the actual project structure (removed non-existent `pkg/` directory).
- **Maintenance**: Performed a full public release audit and documentation revision.

## [1.23.0] - 2026-01-27

### Added
- **History Pruning (50% Strategy)**: Implemented a cost-optimized pruning strategy. When `MAX_HISTORY_TURNS` is hit, history is now pruned down to 50% of the limit to create a stable context prefix for Gemini caching.
- **Amnesia Recovery**: Injected a system-level "Urgent Notice" that instructs the model to use the **Scratchpad** to recover situational awareness after a major history cleanup.
- **Gemini 3 Pricing**: Updated the `estimate_cost` tool and local pricing engine to support Gemini 3 Preview rates and tiered input pricing (Standard vs. Premium at 128k tokens).
- **Safety**: Implemented pre-limit **System Warnings** for the AI model. Escalating notices are injected into the volatile history when `MAX_TURNS` or `MAX_HISTORY_TOKENS` (90%+) limits are nearing.
- **Safety**: Added `ErrContextLimitExceeded` and `ErrMaxTurnsReached` for graceful error handling, replacing `os.Exit(1)` inside the agent library.
- **UI/UX**: Added usage/limit indicators to system logs (e.g., `[System (3/20)]` and `[System (55k/120k)]`).
- **UI/UX**: Sequenced `[Tool Action]` headers to ensure they appear before any interactive prompts, preventing log interleaving.
- **CLI**: Added support for `-l` without an argument to show the last conversation message.
- **Persistence**: Made `bypass_confirmation` state session-persistent by saving it to `output/<session>.bypass`.

### Changed
- **Architecture**: Implemented deep-cloning of history payloads to prevent "context rot" by keeping safety warnings out of persistent history files.
- **Documentation**: Fully revised README and all SOPs to reflect new safety, UX, and context management standards.

## [1.22.0] - 2026-01-27

### Changed
- **Documentation**: Fully revised and synchronized all Standard Operating Procedures (SOPs) and README.md.
- **Go Toolchain**: Updated all documentation to require Go 1.24+ for alignment with `go.mod`.
- **Backend Clarification**: Removed deprecated Gemini Developer API (AI Studio) references from README and SOPs, focusing exclusively on Vertex AI as the primary provider.
- **Consistency**: Fixed various typos, structural inconsistencies, and outdated code templates across the SOP library.
- **Version Bump**: Promoted to v1.22.0.

## [1.21.0] - 2026-01-27

### Added
- **Automation Tools**: Added `bypass_confirmation` tool to disable interactive security prompts, enabling fully automated workflows for trusted agents.
- **Audit Logging**: Implemented persistent command logging. All `execute_command` usage is now recorded in `output/command-log.json` for security auditing.
- **Unified Auditing**: Consolidated destructive actions and bypass events into a unified audit logging stream.

### Fixed
- **Terminal Compatibility**: Improved `stty` state handling for better portability across different shell environments.
- **Error Handling**: Enhanced error reporting and resource management for command logging.

### Changed
- **Version Bump**: Promoted to v1.21.0.

## [1.20.1] - 2026-01-26

### Changed
- **SOPs**: Explicitly mandated E2E verification in the Public Release lifecycle.
- **Maintenance**: Minor versioning corrections.

## [1.20.0] - 2026-01-26

### Added
- **Testing**: Implemented a comprehensive E2E testing suite in `tests/e2e/`, covering CLI fundamentals, session archiving, stdin piping, and multi-turn tool orchestration.
- **Security**: Added `IsPathSafe` with `filepath.Clean` and `filepath.EvalSymlinks` to prevent path traversal and symlink-based boundary escapes.
- **Safety**: Implemented `ConfirmDestructiveAction` gate for `write_file` and `replace_text`.
- **UI/UX**: Enhanced `readSingleKey` to use `/dev/tty`, ensuring interactive tools remain functional even when `os.Stdin` is redirected.
- **Architecture**: Refactored `main.go` into a thin entry point, moving application lifecycle and wiring to `internal/cli`.
- **E2E Mocking**: Added `TELL_ME_MOCK_URL` and `TELL_ME_MOCK_ANSWER` environment variables for offline, automated validation of AI logic and interactive prompts.

### Changed
- **SOPs**: Fully revised and synchronized project Standard Operating Procedures (Testing, Security, Architecture, Agentic Capabilities) to reflect the new hardened standards.
- **Version Bump**: Promoted to v1.20.0.

## [1.18.0] - 2026-01-26

### Added
- **CLI**: Added `-l <int>` flag to display the last N messages from conversation history.
- **UI**: Integrated `glamour` for high-quality Markdown rendering of history entries with role-based coloring (Blue for User, Magenta for Model).
- **History Management**: Implemented `showHistory` in `main.go` to provide a standalone view of the session context.

### Changed
- **Version Bump**: Promoted to v1.18.0.

## [1.17.0] - 2026-01-26

### Added
- **Core Stability**: Improved `Agent.Chat` orchestration loop for more robust multi-modal and authentication handling.
- **Multimodal Optimization**: Optimized history management for tool-generated media. Function responses and injected image blobs are now merged into a single `user` turn to strictly comply with role alternation standards and prevent context fragmentation.
- **Auth Robustness**: Hardened automatic token refresh logic to ensure seamless session continuity during long-running tasks.

### Fixed
- **Role Alternation**: Fixed a "role alternation violation" error when tools returned multiple parts (e.g., text and images) simultaneously.
- **Image Injection**: Fixed a bug where injected image blobs were occasionally dropped or caused history corruption.

### Changed
- **Logging**: Refactored the tool engine's terminal logging to provide clearer turn tracking (`[Tool Engine (turn/max)]`) and summarized tool arguments for better observability.
- **Version Bump**: Promoted to v1.17.0.

## [1.16.0] - 2026-01-26

### Added
- **Financial Metrics (Dynamic Pricing)**: 
    - Added `estimate_cost` tool to provide detailed session cost breakdowns.
    - Added `get_cost_summary` tool with a persistent local ledger (`output/global_costs.json`) for daily expenditure tracking.
    - Implemented a dynamic pricing engine that fetches live Vertex AI rates from GitHub with local 24-hour caching.
    - `get_cost_summary` now automatically updates the current session's cost before displaying the report.
- **Security**: 
    - Centralized and exported `IsPathSafe` in `internal/tools`.
    - Implemented `RegisterSafePath` to allow explicit authorization of directories (like `TELL_ME_HOME`) and files (like session logs) at startup.
    - Hardened `estimate_cost` to securely access logs outside the working directory.

### Changed
- **SOP Organization**: Reorganized the `SOP/` directory into functional subdirectories: `standards/`, `technical/`, `agent/`, and `lifecycle/`. All internal cross-references have been updated.
- **Documentation Standards**: Enforced strict synchronization between the `configs/` directory, CLI help text, and the `README.md` as a pre-release requirement.
- **Version Bump**: Promoted to v1.16.0.

## [1.15.0] - 2026-01-26

### Added
- **UI Controls**: Added `SHOW_THOUGHTS` and `SHOW_TOOLS` visibility controls to `config.yaml` for a cleaner terminal experience.
- **Smart Budget Cap**: Implemented automatic safety clamping for `THINKING_BUDGET`. The tool now automatically adjusts the budget to the model's supported maximum (e.g., 24,576 for Gemini 2.5 Flash, 32,768 for Gemini 3 Flash) to prevent `Error 400` failures.
- **Concurrency**: Added `MAX_CONCURRENT_TOOLS` and `TOOL_TIMEOUT` configuration options.
- **Safety**: Serialized interactive terminal prompts for tool confirmations to prevent UI overlap during parallel execution.

### Fixed
- **API Compatibility**: Corrected Gemini 3 Thinking Budget limit to 32,768 based on explicit API feedback.

### Changed
- **Version Bump**: Promoted to v1.15.0.

## [1.14.1] - 2026-01-28

### Changed
- **Documentation**: Updated `SOP/lifecycle/public_release.md` with explicit branch synchronization and verification steps.
- **Version Bump**: Promoted to v1.14.1.

## [1.14.0] - 2026-01-28

### Added
- **CLI**: Improved `stdin` handling. The tool now correctly detects piped input even when a prompt argument is provided, merging them with a newline.
- **CLI**: Improved multi-line interactive input by using `io.ReadAll`, ensuring more robust capture of `Ctrl+D`.

### Changed
- **Version Bump**: Promoted to v1.14.0.

## [1.13.6] - 2026-01-27

### Fixed
- **Security**: Hardened `isSafeCommand` and `checkPathSafety` to prevent security bypasses via un-anchored regex, absolute paths, or paths embedded in flags.

### Added
- **Safety**: Enhanced validation for read-only auto-approved commands in `execute_command`.

### Changed
- **Version Bump**: Promoted to v1.13.6.

## [1.13.5] - 2026-01-27

### Added
- **Safety**: Implemented binary file detection in `search_files`, `search_usages_globally`, and `list_todos` to avoid payload overflow from non-text files.

### Fixed
- **Stability**: Added output truncation to `search_files`, `read_file`, `grep_definitions`, `execute_command`, `read_url`, and `http_request`. This prevents massive tool responses from exceeding the agent's payload limit (120,000 tokens), which previously caused `Safety Error` crashes.

### Changed
- **Version Bump**: Promoted to v1.13.5.

## [1.13.4] - 2026-01-27

### Changed
- **Documentation**: Updated `README.md` to include information about **Command Safety** (path-based validation) in the Safety Guardrails section.
- **Version Bump**: Promoted to v1.13.4.

## [1.13.3] - 2026-01-27

### Changed
- **CLI UI**: Changed the command text color in the `execute_command` confirmation prompt from yellow to default white for better readability.
- **Version Bump**: Promoted to v1.13.3.

## [1.13.2] - 2026-01-27

### Changed
- **CLI UI**: Aligned colors in the `execute_command` confirmation prompt. "Execute Command:" now uses cyan, and the actual command uses yellow for better visual hierarchy and consistency.
- **Version Bump**: Promoted to v1.13.2.

## [1.13.1] - 2026-01-27

### Changed
- **CLI UI**: Changed the "Executing... (Output shown below)" message color to dark-grey for better visual consistency.
- **Version Bump**: Promoted to v1.13.1.

## [1.13.0] - 2026-01-27

### Added
- **Security**: Implemented path-based safety validation for whitelisted commands in `execute_command`. Commands like `cat`, `grep`, and `ls` now require manual user confirmation if they attempt to access files outside the current working directory or system temp folders.

### Changed
- **CLI UI**: Changed the "Execute Command" prompt color to bold white for better visibility during safety gates.
- **Version Bump**: Promoted to v1.13.0.

## [1.12.9] - 2026-01-26

### Changed
- **Media Tools**: 
    - Updated `create_image` to support an optional `model` parameter, allowing selection between different Imagen models (e.g., `imagen-3.0-generate-001`, `imagen-3.0-fast-001`).
    - Refined `read_image` documentation for better clarity on vision analysis.
- **Version Bump**: Promoted to v1.12.9.

## [1.12.8] - 2026-01-25

### Changed
- **CLI UI**: Removed the echoing of the user's input prompt. Replaced it with a cleaner "Input captured. Processing..." message to reduce terminal noise, especially for multi-line inputs.
- **Version Bump**: Promoted to v1.12.8.

## [1.12.7] - 2026-01-24

### Changed
- **Payload UI**: Changed the payload token warning color from light-grey to **red** when exceeding 90% of `MAX_HISTORY_TOKENS` for higher urgency visibility.
- **Version Bump**: Promoted to v1.12.7.

## [1.12.6] - 2026-01-23

### Changed
- **Payload UI**: Added conditional coloring for the payload token estimate. The token count is now displayed in light-grey when it exceeds 90% of the `MAX_HISTORY_TOKENS` limit, providing a visual warning of approaching context limits.
- **Version Bump**: Promoted to v1.12.6.

## [1.12.5] - 2026-01-22

### Changed
- **Metrics UI**: Refined turn-by-turn metrics logging with conditional coloring.
    - Duration is now always displayed in light-grey.
    - Hits and Misses are highlighted in light-grey when Misses (M) exceed Hits (H) for better visibility of cache efficiency.
- **Version Bump**: Promoted to v1.12.5.

## [1.12.4] - 2026-01-21

### Changed
- **State Tools**: Removed the "session" scope from `manage_scratchpad` and `manage_tasks`. These tools now operate exclusively on global files to simplify persistent context management.
- **Version Bump**: Promoted to v1.12.4.

## [1.12.3] - 2026-01-20

### Removed
- **Video Suite**: Removed the `create_video` tool and underlying `GenerateVideos` API logic to streamline the media package.
- **Cleanup**: Removed unused dependencies related to video generation polling.

### Changed
- **Version Bump**: Promoted to v1.12.3.

## [1.12.2] - 2026-01-15

### Changed
- **UI/UX**: Refactored Markdown rendering to rely entirely on the `glamour` library, improving visual consistency and robustness for complex outputs like nested code blocks.
- **Version Bump**: Promoted to v1.12.2.

## [1.12.1] - 2026-01-10

### Added
- **Safety Documentation**: Added `SOP/lifecycle/self_update_safety.md` to define protocols for agentic self-modification.
- **Compliance**: Applied missing SPDX-License-Identifier headers to all SOP documentation.

### Changed
- **Documentation**: Updated `README.md` and `agentic_capabilities.md` with detailed descriptions of parallel tool execution and multi-modal handling.
- **Version Bump**: Promoted to v1.12.1.

## [1.12.0] - 2026-01-18

### Added
- **Video Suite**: Integrated Veo 2.0 generation capabilities via `create_video` tool.
- **Rollback Tool**: Added `rollback_last_turn` to manually undo the previous interaction.
- **Enhanced Metrics**: Improved token tracking for multimodal and thinking models.

### Changed
- **Version Bump**: Promoted to v1.12.0.

## [1.11.0] - 2026-01-10

### Added
- **Image Suite**: Integrated Imagen 3 generation via `create_image`.
- **Vision Support**: Enhanced multi-modal capabilities for analyzing local images via `read_image`.

## [1.9.0] - 2026-01-05

### Added
- **Performance & Security Tools**: Added `run_benchmark`, `check_vulnerabilities`, and `get_package_graph`.
- **Complexity Analysis**: Added `analyze_complexity` for Go functions.

## [1.7.0] - 2026-01-25

### Added
- **Documentation & Networking**: Added `go_doc` for symbol documentation and `http_request` for custom API interactions.

## [1.6.0] - 2026-01-15

### Added
- **Refactoring & Testing Suite**: Added `rename_symbol`, `get_coverage`, `run_linter`, and `list_todos`.

## [1.5.0] - 2026-01-26

### Added
- **Advanced Intelligence**: Added `search_usages_globally`, `semantic_diff`, and `manage_tasks` (global scope).

## [1.4.1] - 2026-01-26

### Added
- **Enhanced Git & Testing**: Added `get_git_commit`, `get_git_blame`, and `run_tests` for automated test execution.

## [1.4.0] - 2026-01-26

### Added
- **Pretty Markdown Rendering**: Integrated the `glamour` library to provide high-quality, styled terminal output for model responses.
    - Automatic detection of terminal background (Light/Dark) for optimal styling.
    - Selective rendering: Only descriptive text is styled, while code blocks are preserved "as is" with raw backticks to ensure readability and ease of copying.
    - Support for terminal emojis and bold/italic formatting.

### Changed
- **Version Bump**: Promoted to v1.4.0.

## [1.3.0] - 2026-01-26

### Added
- **Intelligence Suite (AST-based code analysis)**: Implemented IDE-level repository mapping for Go projects.
    - \`find_usages\`: Project-wide symbol reference search.
    - \`list_implementations\`: Map interface-struct relationships.
    - \`get_type_info\`: Deep dive into Go types (fields, methods, tags).
- **Go AST Integration**: Upgraded existing tools to use Go's native parser for 100% accuracy in Go files.
    - \`grep_definitions\`: Refactored to use \`go/ast\` for Go source while maintaining regex fallback for other languages.
    - \`get_file_skeleton\`: Upgraded to extract precise signatures, receiver types, and docstrings via AST traversal.
- **Unit Testing**: Comprehensive test suite for AST-based analysis in \`internal/tools/intelligence_test.go\`.

### Changed
- **Version Bump**: Promoted to v1.3.0 to reflect the addition of language-intelligent features.

## [1.2.0] - 2026-01-26

### Added
- **Concurrent Tool Execution Engine**: Optimized performance by executing multiple model-requested tools in parallel.
    - Implemented a semaphore-based worker pool to control concurrency levels.
    - Added per-tool timeout protection using `context.Context` to prevent hanging executions.
- **Configuration Expansion**: Added new parameters to `configs/vertex.yaml`:
    - `MAX_CONCURRENT_TOOLS`: Controls the parallel worker pool size (default: 5).
    - `TOOL_TIMEOUT`: Defines the maximum duration for a single tool call (default: 30s).

### Changed
- **Agent Orchestration**: Refactored the internal loop to collect and synchronize parallel tool results while maintaining conversational order.
- **UI Improvements**: Added real-time feedback for the tool engine's parallel execution status.
## [1.1.0] - 2026-01-26

### Added
- **Repository Mapping Toolset**: Implemented a 4-tool stack for efficient navigation of large codebases.
    - `get_file_skeleton`: Extracts function signatures, classes, and docstrings without reading full file content (significant token savings).
    - `grep_definitions`: Cross-language indexer to find code definitions (Go, Python, JS, Bash).
    - `find_file`: Rapid discovery of files via name patterns.
    - `get_tree`: Enhanced structural map of the repository.

### Changed
- **Tool Registry**: Optimized tool registration for better categorization and help text.
- **Version Bump**: Promoted to v1.1.0 to reflect significant feature expansion.
## [1.0.3] - 2026-01-26

### Changed
- **Tool Documentation**: Refined the `replace_text` tool description to explicitly state that it replaces only the first occurrence found. This improves model reliability during code editing.

### Fixed
- **Version Management**: Bumped internal version constant and synchronized documentation for release.
## [1.0.2] - 2026-01-26

### Fixed
- **History Pruning**: Refined the `Prune` logic to force `removeCount` to be an even number. This ensures that the conversation history always starts with a "user" role, eliminating the "role alternation violation" error.
- **Thinking Configuration**: Standardized `ThinkingLevel` as the primary reasoning control. `ThinkingBudget` and `ThinkingLevel` are now treated as mutually exclusive to prevent model conflicts and 400 errors.
- **Metrics**: Removed redundant heuristic word-counting for thinking tokens in `SendChat`. The application now relies exclusively on the native `UsageMetadata` provided by the GenAI SDK.

## [1.0.1] - 2026-01-26

### Fixed
- **History Pruning**: Corrected logic in `Prune` to ensure an even number of messages are removed, maintaining proper "user" role start and preventing role alternation errors.
- **Thinking Configuration**: Updated `SendChat` to properly initialize `ThinkingConfig` if only a `THINKING_LEVEL` is provided without a budget. Both parameters are now sent to the SDK when defined.
- **Native Metrics**: Integrated native `ThoughtsTokenCount` from the GenAI SDK into the metrics collection logic, ensuring accurate tracking of reasoning usage.

## [1.0.0] - 2026-01-26

### Added
- **Major Tool Suite Expansion**: Reached feature parity with the original Bash assistant.
    - **FileSystem**: Added `search_files`, `replace_text`, and `get_tree`.
    - **Git**: Added `get_git_status`, `get_git_diff`, and `get_git_log`.
- **Improved Observability**: Integrated new tools into the registry and main orchestration loop.

### Changed
- **Version Bump**: Promoted to v1.0.0.

## [0.9.9] - 2026-01-26

### Changed
- **Config Standardization**: Updated `configs/vertex.yaml` to include all available configuration variables with sensible defaults, aligning it with the latest safety features.
- **Version Bump**: Updated binary version to v0.9.9.

## [0.9.8] - 2026-01-26

### Added
- **Safety Rollback**: Implemented `MAX_HISTORY_TOKENS` check. If a payload exceeds the limit, the tool now rolls back the history to its last known good state and exits with an error.
- **Recursion Limit**: Implemented `MAX_TURNS` (MaxToolTurns) to prevent infinite tool calling loops.
- **Config Clarification**: Separated `MAX_HISTORY_TURNS` (for turn-based pruning) from `MAX_TURNS` (for tool call limits).

### Changed
- **Version Bump**: Updated binary version to v0.9.8.

## [0.9.7] - 2026-01-26

### Changed
- **UI Cleanup**: Removed the redundant "Generated in 0.000s" metric from the pre-API log, as Go's execution time for payload preparation is negligible.
- **Version Bump**: Updated binary version to v0.9.7.

## [0.9.6] - 2026-01-26

### Fixed
- **Improved Payload Estimation**: Refined the `Payload` token estimation heuristic to be significantly more accurate.
    - Now includes tool declarations, system instruction overhead, and tool execution results (JSON).
    - Adjusted the character-to-token ratio from 4.0 to 3.2 to better reflect technical/JSON content.
- **Version Bump**: Updated binary version to v0.9.6.

## [0.9.5] - 2026-01-26

### Changed
- **Enhanced Pre-API Logging**: Replaced the generic `[API] Calling Gemini...` message with a detailed payload summary.
    - New format: `[System] Payload: ~<TOKENS> tokens | Generated in <SECONDS>s`.
    - Includes a heuristic-based token estimation for better transparency.
- **Version Bump**: Updated binary version to v0.9.5.


### Fixed
- **Usage Logging Robustness**: Usage metrics are now logged even if the API returns an error (e.g., safety filter block), provided that usage metadata is present in the response.
- **Text Output during Tool Calls**: Fixed a bug where text parts of a model response were skipped if the response also contained function calls. Text is now printed immediately when received.
- **Thinking Token Approximation**: Refined thinking token calculation to ensure it correctly contributes to the "New" (N) token total.
## [0.9.3] - 2026-01-26

### Added
- **Session Archiving**: Implemented automatic backup of session files (history, logs, scratchpads, tasks) when starting a new session with the `-new` flag. Files are moved to timestamped directories in `output/backups/`.

### Changed
- **Unified Naming Alignment**: Aligned file naming conventions with the original Bash project for full cross-compatibility.
    - History: `output/<MODE>_history.json`
    - Log: `output/<MODE>_tokens.log`
    - Scratchpad: `output/<MODE>_scratchpad.md`
    - Tasks: `output/<MODE>_tasks.json`
- **Version Bump**: Updated binary version to v0.9.3.
## [0.9.2] - 2026-01-26

### Changed
- **UI Logging Refinement**: Added `[API] Calling Gemini...` and `[Tool Action]` messages to stderr to match the original Bash project's logging style. This provides better visibility into the multi-turn orchestration loop.

## [0.9.1] - 2026-01-26

### Fixed
- **Auth Token Expiration**: Implemented automatic retry logic for `401 UNAUTHENTICATED` errors. If a cached token expires, the assistant will now automatically clear the cache, retrieve a fresh token via `gcloud`, and retry the request once without user intervention.

## [0.9.0] - 2026-01-26

### Added
- **Metrics Logging**: Implemented turn-by-turn token usage logging to `.log` files in the `output/` directory, matching the Bash version's format.
- **Cost Estimation Tool**: Added the `estimate_cost` tool, allowing the agent to calculate and report the estimated USD cost of the current session.
- **Real-time Metrics**: Usage statistics (Hits, Misses, Completion, Total, Duration) are now printed to stderr in gray after every API turn.

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
    - `SOP/technical/architecture_and_packages.md`: Now mandates business logic resides in testable packages.
    - `SOP/standards/testing_standards.md`: Now mandates mocking complex, multi-part API responses.
    - `SOP/agent/agentic_capabilities.md`: Explicitly mandates preserving model thoughts and signatures to prevent Vertex AI 400 errors.

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
