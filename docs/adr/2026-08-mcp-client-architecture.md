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

Each server declares an explicit `AUTH` mode — `"auto"`, `"gh"`, `"bearer"`,
`"basic"`, or `"none"` (default `"auto"`) — that controls how its bearer token
is resolved at client-construction time:

- `auto` — an explicit `TOKEN` wins; otherwise a GitHub-hosted endpoint
  resolves via `gh auth token` / `GITHUB_TOKEN`, and any other endpoint uses no
  authentication.
- `gh` — an explicit `TOKEN` wins; otherwise the token is resolved via
  `gh auth token`, then the `GITHUB_TOKEN` environment variable.
- `bearer` — the explicit `TOKEN` is required (an empty `TOKEN` is rejected at
  config validation).
- `basic` — `USERNAME` and `TOKEN` are both required (an empty either is
  rejected at config validation); every request carries
  `Authorization: Basic base64(username:token)`. Credentials support
  `${ENV_VAR}` interpolation via the config loader's `expandEnvHook`;
  plaintext credentials must never be committed to configs.
- `none` — no authentication is attached.

Across all token-resolving modes, resolution uses a hierarchy: an **explicit
`Token`** on the server config wins, then a cached token (memoized across
servers so `gh` is not spawned repeatedly), then `gh auth token`, then the
`GITHUB_TOKEN` environment variable. The DI layer resolves credentials during
client construction and skips (with a warning) a server whose credentials
cannot be resolved, rather than failing startup for the whole agent.

**Amendment (2026-08, issue #1389 — Basic auth mode):**

1. **Single factory-owned normalization.** The mode→credentials mapping is
   owned entirely by `resolveServerToken` in the DI factory, which returns
   a credential triple `(username, token, ok)`. Stray `USERNAME` under
   non-basic modes is silently normalized away (username is `""` unless the
   mode is `basic`), matching the existing `TOKEN`-under-`none` tolerance —
   validation is positive-rule only, with no negative rules.
2. **Unresolvable-under-basic is a config-load error, not a factory state.**
   `validate()` hard-rejects `AUTH: basic` with an empty `USERNAME` or
   `TOKEN` before the factory runs; the `basic` arm of `resolveServerToken`
   is an unconditional pass-through. The warn-and-skip path
   (`mcp_token_resolution_skipped`) remains reachable only from `gh`/`auto`
   resolution failures.
3. **Config-loader debug dumps — RESOLVED (issue #1393).** The two
   debug-gated dumps in `internal/infrastructure/config/config.go` — the
   raw-content dump and the viper parsed-key dump with `slog.Any` —
   previously serialized config values when debug logging was enabled, and
   were recorded here (as of #1389) as a known exception to the
   `mcp-token-not-logged` scope with a tracked follow-up (#1390). Issue
   #1393 supersedes that framing and closes the gap. **Corrected framing:**
   the dumps were an **exception to the scope** of `mcp-token-not-logged`
   (whose subject is the MCP credential plumbing: config validation, DI
   factory, client transport — verified clean), **not a violation**;
   post-fix the loader honors the invariant's *intent* on the `MCPServer`
   credential surface as a side effect of general diagnostics hardening
   while remaining outside its literal plumbing scope. The invariant
   statement and the domain model are unchanged. **Wording:** the **two
   secret-bearing debug dumps now redact secret-bearing values**.

   **Verified exposure class (issue #1393):** the dumps serialize YAML-file
   config values — plaintext secret-bearing values when present in the file —
   plus `${ENV_VAR}` reference literals (the reference name, never the
   resolved value). The original #1390 claim that `AutomaticEnv`-overridden
   live secrets reach the parsed-key dump was incorrect for the current
   wiring: the dump runs inside `readConfigFile`, which `configureViper`
   calls **before** `SetEnvPrefix`/`SetEnvKeyReplacer`/`AutomaticEnv`, and
   viper's env lookup is gated on `automaticEnvApplied` (viper v1.21
   `find()`); the explicit `BindEnv` map is also populated only after the
   dump. The deny-list redaction is future-proof: if a future refactor moves
   the dump after env wiring, env-overridden secrets are redacted by the
   same path (the liveness test's `env-super-secret` guard becomes live
   under that wiring).

   **Redaction contract (issue #1393):** the parsed-key dump routes each key
   through a deny-list (`isSecretKey` — suffix-anchored on the leaf,
   case-insensitive; `api_key`/`auth_token`/`authorization`/`token`/
   `password`/`secret`/`credential`/`username`/`key` families incl. plurals;
   `max_tokens`/`max_history_tokens` deliberately excluded) and logs
   `value=[REDACTED]` for a deny-listed leaf while keeping the key visible;
   the raw-content dump routes through a line-oriented parser
   (`redactRawContent`) that redacts deny-listed key values (preserving the
   key), suppresses continuation lines, handles colonless lines, flow maps
   and brace-less subkey chains (value-mode scanning gated on unquoted
   scalars so quoted prose/URLs pass byte-identically). Residual class
   (accepted, pinned by boundary tests): (i) innocuous-name scalars (e.g.
   `PAYLOAD: sk-1234`); (ii) key-shaped malformed-block content inside a
   block scalar (fail-closed over-redaction); (iii) barred `tokens:` flow
   sub-keys (kept off the deny-list to protect `max_tokens`). Unquoted
   plain-scalar URLs containing `token:` are over-redacted — an accepted,
   documented cosmetic trade (fail-closed), compensated in valid YAML by the
   parsed dump (`url` leaf not deny-listed).
4. **Hot-reload boundary unchanged.** `MCP_SERVERS` remains excluded from
   hot-reload (§10): changing Basic credentials (or any server
   configuration) requires starting a new session.

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

**Amendment (2026-09, issue #1378 — empty-type schema serialization):**

1. **Root vs nested (unchanged root rule):** Root-level combinators
   (`oneOf`/`anyOf`/`allOf`/`$ref`) and root union types still degrade to
   `Parameters=nil` freeform args with a `slog.Warn` — per the original §6.
2. **Nested unrepresentable nodes → untyped ANY:** Nested combinators, union
   types, `"null"`, or absent `type` inside `properties`/`items` are **not
   dropped** — `convertObject` keeps them as untyped ANY nodes
   (`tools.Schema{Type: "", Description: ..., Enum: ...}`). `required` is
   never pruned (nothing is dropped, so it never dangles).
3. **Empty `Type` is the seventh "ANY" representation:** wire adapters must
   **omit the `type` key**, never emit `""`. OpenAI
   (`internal/infrastructure/llm/openai/client.go` — `json:"type,omitempty"`
   on the `schema` wire struct) and Gemini (the genai SDK's own
   `json:"type,omitempty"` on `genai.Schema.Type`) omit via `omitempty`. The
   **Anthropic root** — whose API mandates `"type":"object"` on
   `input_schema` (always emitted) — forces the object type while
   **preserving** converted content (properties/description/enum); the
   nil-`Parameters` fallback `{"type":"object","properties":{}}` is a pure
   API-contract shim for genuinely-absent schemas. The root/nested asymmetry
   is principled: nested nodes sit inside a typed container (untyped-any
   children are valid JSON Schema), while the root *is* the tool's parameter
   contract.
4. **Accepted cost:** nested combinator branches flatten to ANY, branch-level
   validation is lost, and call-time server errors for required params are
   possible — but the LLM can still attempt them (vs. today: zero inference
   with MCP enabled).
5. **Gemini note:** `genai.Type("")` is the empty string; the SDK's
   `omitempty` omits the key. `TYPE_UNSPECIFIED` must **never** be set
   explicitly (it would serialize as a non-empty
   `"type":"TYPE_UNSPECIFIED"`).
6. **Open-question resolutions (recorded per the issue):** (a) issue
   verification includes an actual `run_secret_scanning` call, not merely
   discovery + inference; (b) the redundant Gemini T2 guard was dropped —
   the pinning test (`TestToSDKSchema_EmptyType_OmitsTypeKey`) is the
   documentation.

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

### 11. Tool Authorization — `mcp_` Prefix Allowance (Amendment 2026-10)

1. **Decision:** MCP-discovered tools — namespaced `mcp_<server>_<tool>` per
   §5 — are now permitted by the tool authorizer. Discovered-but-undenied
   tools are dispatchable end-to-end: the #1378/#1379 schema fix made them
   discoverable; this amendment makes them authorizable.
2. **Implementation:** `domain.Policy.AllowedCommandPrefixes` (default
   `["mcp_"]`) and `domain.Policy.IsToolAllowed(name)` (exact allowlist match
   OR prefix match) in `internal/domain/security/policy.go`; enforced via
   `Manager.IsToolAllowed` (`internal/infrastructure/security/manager.go`
   delegation) in `securityAuthorizer.Authorize`
   (`internal/agent/executor/authorizer.go`). `Policy.IsCommandAllowed`
   remains **exact-match** (fail-closed) — shell command validation
   (`SafetyService.IsCommandSafe` → `command_validator.go`) is byte-identical
   and unaffected.
3. **Consent unchanged:** the whitelist gate is separate from consent (per the
   original §8). Consent remains per-tool via `RequiresConsent`; readonly
   endpoints default `requiresConsent: false` — so `mcp_github_*` tools
   dispatch without prompting, a deliberate, recorded posture consequence —
   while non-readonly servers still prompt. `AutoApprovableCommands` is
   untouched: `mcp_` tools are not auto-approvable.
4. **Security rationale:** the `mcp_` prefix is a reserved namespace produced
   only by `FormatToolName` (ADR-067 §5 — server keys
   `^[a-z0-9-]{1,24}$`, tool names from server discovery); built-in tool
   names never start with `mcp_`, so there is no collision; matching is
   case-sensitive; every other name remains fail-closed.
5. **Accepted trade-off:** the prefix check is a string-prefix allowance, not
   a proof of MCP provenance — any in-process plugin registering a `mcp_*`
   name would also pass, which is accepted because plugins are trusted
   in-process code and the alternative (provenance tracking) adds registry
   coupling disproportionate to the threat model.

### Amendment (2026-08, issue #1396 — stdio transport for local MCP servers)

1. **Second transport implementation.** tell-me-go now supports local stdio
   MCP servers via a separate `StdioClient` type
   (`internal/infrastructure/mcp/stdio_client.go`) in the same package — not
   a mode on the HTTP `Client`. Eager construction: `NewStdioClient(cfg,
   logger)` spawns `COMMAND` with `ARGS`/`DIR`/`ENV` via
   `exec.CommandContext`, connects over
   `sdkmcp.IOTransport{Reader: stdoutPipe, Writer: stdinPipe}` inside the
   constructor, bounded by the server's `TIMEOUT` (default 300s). One child
   process per server; no reliance on `os.Stdin`/`os.Stdout` (the SDK's
   `StdioTransport` is hard-wired to those, one per process, hence rejected).
   Reuses the HTTP client's unexported conversion helpers; SDK confinement
   (§2) unchanged.

2. **Lifecycle & process-tree guarantees (two-tier).** Direct-child reaping
   is **deterministic**: a reaper goroutine started immediately after
   `cmd.Start()` sends `cmd.Wait()`'s result to a buffered channel, so the
   child is reaped the moment it dies — zombie prevention is not contingent
   on `Close()`. Tree termination is **best-effort** via shared-pipe stdin
   EOF: launchers (`npx`, `uvx`) pass stdio through rather than proxying it,
   so a grandchild sees EOF directly and SDK-built servers
   (`server.Run(ctx, &StdioTransport{})` read loop exits on `io.EOF`)
   self-terminate; the Unix SIGPIPE write-backstop covers a grandchild that
   writes after our read end closes. **Zombie vs orphan**: no zombies,
   guaranteed; orphans possible for transport-contract-violating servers (an
   EOF-ignoring **and silent** grandchild on Unix; any EOF-ignoring
   grandchild on Windows — no signal backstop) — accepted residuals. **No
   process-group kill this PR** (Unix-only platform split for a
   misbehaving-server edge; pgid-kill is wrong for a reparenting child;
   Windows needs Job Objects) — tracked future option.

3. **Fast-death and failure surfaces.** A non-blocking pre-check at the top
   of `ListTools`/`CallTool` (under the client mutex) returns a sticky
   `mcp: stdio child %q exited: %w` error once the child has died — every
   subsequent call fails fast deterministically (the SDK alone does not
   guarantee this). In-flight EOF (`io.EOF`/`ErrConnectionClosed`)
   coinciding with a confirmed child exit is annotated with the exit status;
   a wedge surfaces as the plain `context.DeadlineExceeded` wrap — dead vs
   wedged are distinguishable by error text and latency.
   Spawn/connect/handshake failures and handshake timeout surface at DI
   `Build` as the existing warn+skip (server skipped for the session;
   `MCP_SERVERS` read once, §10 unchanged); per-call timeouts are
   recoverable (call fails, session remains); **no kill on
   operation-timeout** — a slow legitimate call (e.g. a 200s filesystem op
   under TIMEOUT 300) is indistinguishable from a wedge and does not poison
   the session; documented known limitation, child stderr is the operator's
   visibility window.

4. **Close semantics.** `Close()` is idempotent and mutex-guarded, in this
   order: (1) `session.Close()` first — closes both pipes, sending stdin EOF
   so a well-behaved child (and its tree) exits gracefully; (2) `cancel()` as
   the kill backstop for uncooperative children (called explicitly and
   deferred so it fires even on an error unwinding `session.Close`);
   (3) join the reaper (expected `signal: killed`/`context.Canceled` logged
   at debug, not surfaced). `factory.Close()` iterates tracked clients, so
   session teardown kills every child.

5. **Consent/serial defaults — trusted local spawns (MAINTAINER DECISION).**
   Stdio servers are trusted by virtue of config trust: `consent=false`
   default (no user prompt), `serial=true` default (mutating local
   processes), with `REQUIRES_CONSENT: true` opting back in. **This deviates
   from the #1394 grill round's recommendation (`consent=true` default)** —
   flag the deviation explicitly and record the maintainer's rationale: a
   `COMMAND`/`ARGS` config entry is an explicit, operator-authored grant of
   local execution — no different in trust class from the config's own
   `SELECTED_PROVIDER` — and prompting on every call would make "many local
   MCP services" tedious. `EffectiveRequiresConsent()` gains an explicit
   stdio branch (override wins, then `IsStdio()` → false, else
   `!isReadOnly()`); `EffectiveSerial()` remains `!isReadOnly()` — stdio has
   no URL, so it already yields `true`. **Known consequence (accepted):** a
   read-only local server (e.g. `fetch`) is forced serial forever — there is
   no serial override for any transport today (`EffectiveSerial()` is
   global; consent is the only overridable axis), so stdio is not made
   *more* configurable than HTTP. A per-server serial override is a tracked
   future option: if ever added, it must apply to both transports or carry a
   justification for asymmetry.

6. **Mode-conflict validation rule.** `validate()` rejects
   `AUTH: bearer`/`basic` together with `COMMAND` ("stdio (COMMAND) servers
   transmit no credentials"), checked before the positive credential rules
   so the mode-conflict message wins. This is a mode-conflict rule, same
   family as COMMAND/URL mutual exclusivity — it does not disturb §4's
   positive-rule-only credential philosophy; `auto`/`gh`/`none`/empty under
   stdio are accepted (inert under the stdio short-circuit, which also means
   `gh`/`auto` under stdio never resolve and never invoke the resolver), and
   stray `TOKEN`/`USERNAME` under stdio remain tolerated (§4 stray-field
   tolerance). COMMAND and URL are mutually exclusive — exactly one must be
   set.

7. **Concurrent construction.** The DI factory
   (`internal/infrastructure/di/mcp_factory.go`) builds clients in two
   phases: a sequential token pre-pass (stdio short-circuits first;
   `gh`/`auto` under stdio never resolve; HTTP servers resolve via the
   single-flight `gh`-memoized resolver, credentials stamped into per-server
   copies), then concurrent construction per server (`sync.WaitGroup`,
   per-server spawn+handshake bounded by `TIMEOUT`), failures warn+skip per
   server, no overall `Build` bound. First-`GetRegistry` latency caps at
   `max(Tᵢ)` + plugin discovery rather than ΣTᵢ. **Nuance:** the plugin's
   own 30s per-server discovery bound (`defaultDiscoveryTimeout`) is separate
   from and additional to `TIMEOUT` — a server whose handshake took the full
   `TIMEOUT` still gets its own 30s `ListTools` window.

8. **Stderr scope.** Child stderr is always logged at Info with
   `mcp_server=<name>`, line-buffered, and never parsed as JSON-RPC. This is
   **outside** the `mcp-token-not-logged` invariant (the invariant governs
   tell-me-go's own credential plumbing — config validation, DI factory,
   client transport — not arbitrary child output); a misbehaving child that
   echoes secrets to stderr is an accepted residual of the wedge/death
   visibility window. The domain model invariant statement is unchanged.

9. **Command resolution semantics.** Bare `COMMAND` (no path separator)
   resolves via `exec.LookPath` in **tell-me-go's own process PATH** — not
   `ENV.PATH`, not `DIR`; separator-bearing `COMMAND` (`/abs`, `./rel`) is
   used as-is, relative resolved against `DIR`; `ENV.PATH` governs the child
   only post-exec (it reaches the server's own subprocess resolution — e.g.
   uvx finding `mcp-server-git` — which is why it is set and why failures
   are confusing). `${VAR}` expands at config load via `expandEnvHook` in
   `COMMAND`, `ARGS`, `DIR`, and `ENV` values. Windows bare-name lookup
   honors `PATHEXT` via the same stdlib path. On `exec.ErrNotFound`, the
   error is annotated with the resolution contract (fail-point only; no
   heuristic warning when `ENV` contains a PATH override).

10. **Tracked future options** (explicitly NOT this PR): (a) per-server
    serial override, applying to both transports or with a justified
    asymmetry; (b) process-group kill (Unix pgid) / Job Objects (Windows)
    for transport-contract-violating grandchildren; (c) extraction refactor
    of `(*MCPServerConfig).validate` (CC=20, cataloged as structural guards
    in `INTENTIONAL_NON_FIXES.md`).

### Consequences

Stdio support broadens the MCP surface from remote-only to local processes
while preserving the SDK-confined, port-based architecture: a separate
`StdioClient` type keeps the HTTP `Client` untouched, and the two-tier
lifecycle (deterministic direct-child reaping + best-effort tree termination
via shared-pipe EOF) prevents zombies with accepted orphan residuals for
contract-violating servers. The trusted-spawn consent default is a deliberate
deviation from the #1394 grill recommendation, justified by the
operator-authored trust class of `COMMAND`/`ARGS`; the forced-serial
consequence for read-only local servers and the no-kill-on-operation-timeout
policy are documented known limitations with tracked future options rather
than silent trade-offs.

## Consequences

**Positive:** the SDK is fully isolated behind `tools.MCPClient`, so the
adapter is swappable and the domain/tools layers stay third-party-free. The
adapter is testable against a real SDK server via `httptest.Server`
(deterministic, stateless, JSON responses), and the three-way error split gives
the LLM robust in-turn recovery from non-terminal MCP tool errors rather than
aborting the whole turn. Schema normalization keeps MCP tool schemas callable
by the existing validation path.

**Negative:** the truncation scheme means very long tool names lose their
human-readable tail (replaced by a hash), root combinator-heavy MCP schemas
degrade to freeform args, losing validation for those tools, and nested
combinators flatten to untyped ANY (branch-level validation lost, but the
tool stays callable). A server whose credentials cannot be resolved is
silently skipped at startup (warned, not fatal) — operators must watch logs
to notice a misconfigured MCP server.
