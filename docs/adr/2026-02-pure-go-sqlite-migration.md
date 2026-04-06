# ADR-016: Migrate Persistent State to Pure Go SQLite

## Status
Proposed

## Context
Currently, TellMeGo manages persistent state (Tasks, Configuration, and History) using flat JSON/JSONL files (`ListStore`, `KVStore`). 
As we introduce concurrent LLM tool execution, flat file persistence poses severe scalability and consistency risks:
1.  **O(N) Operations**: Operations like updating or deleting a task require rewriting the entire list to disk (`WriteAll`). 
2.  **Concurrency Conflicts**: When the LLM executes multiple tools concurrently (e.g., modifying two different tasks simultaneously), file rewrites will lead to race conditions and data loss unless heavy lock contention is introduced at the service layer.
3.  **Portability:** TellMeGo is a CLI tool distributed as a single static binary. Relying on traditional SQLite (via `mattn/go-sqlite3`) requires CGO, which complicates cross-platform builds and breaks the single-binary deployment model.

## Decision
We will migrate the underlying persistence layer for structured state (Tasks, Config, Scratchpad) to a **Pure Go SQLite** implementation using `modernc.org/sqlite`.

1.  **Driver Selection:** We will use `modernc.org/sqlite` because it is a CGO-free port of SQLite. It compiles natively into the Go binary without requiring external C libraries, preserving TellMeGo's portability.
2.  **Database Strategy:** 
    *   A single SQLite file (e.g., `tellmego.db`) will be stored in the configured `TELL_ME_HOME`.
    *   The `database/sql` standard library will interface with the `modernc` driver.
    *   Connections will be configured with `PRAGMA journal_mode=WAL` (Write-Ahead Logging) to allow highly concurrent reads and writes, resolving the concurrent tool execution bottlenecks.
3.  **Schema Migration:**
    *   `tasks` table: `id INTEGER PRIMARY KEY, content TEXT, status TEXT, created_at DATETIME`.
    *   `config` table: `key TEXT PRIMARY KEY, value TEXT`.
    *   `scratchpad` table: `id INTEGER PRIMARY KEY, content TEXT` (singleton row).
4.  **Interface Preservation:** The existing Hexagonal interfaces (`KVStore`, `ListStore`) in the domain layer will remain unchanged. We will implement new SQLite-backed adapters in the `infrastructure/persistence` layer.

## Consequences

### Positive
*   **O(1) Updates:** Task modifications translate to single SQL `UPDATE` or `DELETE` statements.
*   **Concurrency Safe:** SQLite with WAL handles concurrent transactions natively, unblocking the concurrent LLM tool execution architecture.
*   **No CGO:** The build process remains simple and cross-platform compatible.

### Negative
*   **Binary Size:** `modernc.org/sqlite` is a transpiled C-to-Go library, which will slightly increase the compiled binary size (by ~4-5 MB).
*   **Migration Path:** We must implement an auto-migration script on startup to seamlessly transfer existing users' flat JSON files into the new SQLite database before deprecating the flat file implementation.