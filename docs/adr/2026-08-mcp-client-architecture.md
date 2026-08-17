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

Each server declares an explicit `AUTH` mode — `"auto"`, `"gh"`, `"bearer"`, or
`"none"` (default `"auto"`) — that controls how its bearer token is resolved at
client-construction time:

- `auto` — an explicit `TOKEN` wins; otherwise a GitHub-hosted endpoint
  resolves via `gh auth token` / `GITHUB_TOKEN`, and any other endpoint uses no
  authentication.
- `gh` — an explicit `TOKEN` wins; otherwise the token is resolved via
  `gh auth token`, then the `GITHUB_TOKEN` environment variable.
- `bearer` — the explicit `TOKEN` is required (an empty `TOKEN` is rejected at
  config validation).
- `none` — no authentication is attached.

Across all token-resolving modes, resolution uses a hierarchy: an **explicit
`Token`** on the server config wins, then a cached token (memoized across
servers so `gh` is not spawned repeatedly), then `gh auth token`, then the
`GITHUB_TOKEN` environment variable. The DI layer resolves credentials during
client construction and skips (with a warning) a server whose credentials
cannot be resolved, rather than failing startup for the whole agent.

### 5. Namespacing & Shortening

Discovered tools are registered under a deterministic, namespaced name
`mcp_<server>_<tool>`. Server keys are constrained to 1–24 lowercase
alphanumeric/hyphen characters (`^[a-z0-9-]{1,24}$`, enforced at config
validation). When the namespaced name exceeds 64 bytes, the tool segment is
truncated (by runes, capped at 40) and a stable 8-hex-char SHA-256 prefix of
the full tool name is appended, producing `mcp_<server>_<prefix40>_<hash8>`.
This keeps names short enough for LLM token budgets while preserving
uniqueness and determinism across runs.

The 24-character server-key cap is a mathematical guarantee that every
generated tool name stays within 64 bytes. In the truncation path the fixed
segments are `mcp_` (4) + server (≤24) + `_` (1) + tool prefix + `_` (1) +
hash8 (8); the tool prefix is budgeted to `64 − 4 − len(server) − 1 − 1 − 8`
and further capped at 40 runes. With `server` = 24 the prefix budget is 26,
yielding exactly 64 bytes; shorter server names yield a larger (≤40) prefix
budget but a strictly smaller total — so no tool name exceeds 64 bytes.

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

### 10. Session Lifecycle & Hot-Reload Boundary

`MCP_SERVERS` is read once at session startup: the DI layer constructs MCP
clients when the tool registry is first built (lazily) and tracks them so that
session teardown closes every active client connection via the session
`cleanup` closure. Reconfiguring MCP servers — adding, removing, or changing
endpoints, credentials, or auth modes — requires starting a new session; MCP
servers are excluded from mid-turn hot-reload, which remains scoped to the
subset of configuration the per-turn config watcher manages.

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
