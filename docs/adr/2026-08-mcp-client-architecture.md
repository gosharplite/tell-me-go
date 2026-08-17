# ADR-067: MCP Client Architecture — Remote MCP Server Consumption via Streamable HTTP

- **Status**: Accepted
- **Date**: 2026-08
- **Deciders**: Architect, Coder
- **Consulted**: Issue #1373

## Context

tell-me-go consumes external Model Context Protocol (MCP) servers so its LLM
agent can execute tools exposed by remote services. The motivating target is
GitHub's hosted remote MCP server (`https://api.githubcopilot.com/mcp/readonly`),
reached over the Streamable HTTP transport — the transport GitHub's hosted MCP
endpoint uses (standalone SSE is disabled; responses are plain HTTP POST
JSON-RPC exchanges).

The constraint that shapes this decision: the MCP SDK is a third-party
dependency, and the codebase's architectural discipline (ADR-055/060/062)
requires third-party adapters to live behind a domain port so they remain
swappable and never leak into the domain or tools layers. The domain must not
import `github.com/modelcontextprotocol/go-sdk`; only the infrastructure
adapter may.

## Decision

### 1. Client-First Direction

tell-me-go acts as an **MCP client**, never a server. It discovers and invokes
tools on remote MCP servers; it does not expose tools over MCP to others. This
bounds the surface to `ListTools` + `CallTool` + `Close` and keeps the adapter
deliberately small.

### 2. Transport & SDK Confinement

The Streamable HTTP transport is implemented with the official
`github.com/modelcontextprotocol/go-sdk`, and the SDK import is **strictly
confined** to `internal/infrastructure/mcp/`. No other package may import the
SDK. This is the ADR-055/060 injection pattern applied to MCP: the adapter is
the only place that knows the wire protocol.

### 3. Domain Port

The domain port is `tools.MCPClient` in
`internal/domain/tools/mcp_client.go`, with **zero third-party dependencies**.
It exposes `ListTools`, `CallTool`, and `Close`, returning only domain types
(`MCPToolDefinition`, `ToolResult`). The `internal/tools/integrations/mcp`
plugin consumes the port (plus `plugin.MCPServerDependency`) and never imports
the infrastructure layer.

### 4. Authentication & Caching

Tool-call authentication resolves via a hierarchy: an **explicit `Token`** on
the server config wins, then a cached token, then `gh auth token`, then the
`GITHUB_TOKEN` environment variable. The DI layer resolves credentials during
client construction and skips (with a warning) a server whose credentials
cannot be resolved, rather than failing startup for the whole agent.

### 5. Namespacing & Shortening

Discovered tools are registered under a deterministic, namespaced name
`mcp_<server>_<tool>`. When that name exceeds 64 bytes, the tool segment is
truncated (by runes, capped at 40) and a stable 8-hex-char SHA-256 prefix of
the full tool name is appended, producing `mcp_<server>_<prefix40>_<hash8>`.
This keeps names short enough for LLM token budgets while preserving
uniqueness and determinism across runs.

### 6. Schema Normalization

MCP input schemas (JSON Schema) are normalized into the canonical
`tools.Schema` representation with UPPERCASE type names (`OBJECT`, `STRING`,
`INTEGER`, `BOOLEAN`, `ARRAY`, `NUMBER`). Any schema that uses combinators
(`oneOf`/`anyOf`/`allOf`/`$ref`) or a union `type` degrades to
`Parameters=nil` (freeform args) with a `slog.Warn`, because the canonical
schema cannot faithfully express them and an incomplete schema would fail
validation at call time.

### 7. Three-Way Error Split

Tool results follow a three-way split (ADR-022 / issue #1373):

1. **Transport/HTTP/JSON-RPC error** → a **terminal Go error** returned to the
   caller.
2. **MCP-level `isError: true`** → a **non-terminal** `ToolResult.Error` with a
   **nil** Go error, so the LLM can recover in-turn.
3. **Clean success** → `ToolResult{Text, BinaryData, Metadata}` with a nil Go
   error.

### 8. Consent & Concurrency Defaults

Consent and serialization default from the URL class: `/readonly` endpoints
default to `RequiresConsent: false` and concurrent (`Serial: false`);
non-`/readonly` endpoints default to `RequiresConsent: true` and
`Serial: true`. An explicit `REQUIRES_CONSENT` override always wins.

### 9. Timeout & Liveness

Each server has a configurable per-server timeout (default 300s). Registered
MCP tools are marked `LongRunning: true` with `LivenessThreshold: 0`, because
remote tool execution can legitimately block for the full server timeout and
must not be killed by the standard short-tool liveness heuristic.

## Consequences

**Positive:** the SDK is fully isolated behind `tools.MCPClient`, so the
adapter is swappable and the domain/tools layers stay third-party-free. The
adapter is testable against a real SDK server via `httptest.Server`
(deterministic, stateless, JSON responses), and the three-way error split gives
the LLM robust in-turn recovery from non-terminal MCP tool errors rather than
aborting the whole turn. Schema normalization keeps MCP tool schemas callable
by the existing validation path.

**Negative:** the truncation scheme means very long tool names lose their
human-readable tail (replaced by a hash), and combinator-heavy MCP schemas
degrade to freeform args, losing validation for those tools. A server whose
credentials cannot be resolved is silently skipped at startup (warned, not
fatal) — operators must watch logs to notice a misconfigured MCP server.
