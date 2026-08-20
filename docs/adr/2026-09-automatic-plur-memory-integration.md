# ADR-068: Automatic PLUR Memory Integration

- **Status**: Accepted
- **Date**: 2026-09
- **Deciders**: Architect, Coder
- **Consulted**: Issue #1404 (supersedes #1403, which superseded #1402)
- **Amended**: 2026-08-20, issue #1412 — `plur_learn_batch` payload carries
  `max_llm_calls: 0`; `FlushSession` retains buffered episodes on write
  failure (claim/restore)
- **Amended**: 2026-09, issue #1414 — `MEMORY.ENABLED` gates both seams
  (injection AND learning); `FlushSession` drains without writing when
  disabled

## Context

tell-me-go is a generic MCP client: it exposes MCP tools (e.g. `plur_learn`,
`plur_recall`, `plur_inject_hybrid`, `plur_status`) to the agent, but there is
**no hooks mechanism** for an MCP server to inject content into the prompt and
no automatic learning extraction. In any multi-persona deployment where
personas share one `TELL_ME_HOME`:

- A correction made to one persona session is invisible to the others unless
  the agent explicitly calls `plur_learn` and every other agent explicitly
  calls `plur_recall`/`plur_inject_hybrid`.
- Memory quality depends on agent self-discipline, not guaranteed plumbing.
- Competitor integrations (Claude Code hooks, OpenClaw plugin, Hermes plugin,
  DeepSeek Harness plugin) all do this automatically; tell-me-go cannot
  without a feature.

Goal: **"install once, memory works"** — the same value PLUR provides to Claude
Code, delivered through tell-me-go's own extension seams. This issue is the
final implementation spec; it supersedes #1403 (which superseded #1402). The
two-seam design was adversarially reviewed in **two grill rounds** — Round 1 on
#1402 (9 questions, verdict: proceed with changes) and Round 2 on #1403
(5 questions, verdict: proceed with changes) — and **all design decisions,
including the Round 2 refinements, are settled. There are no open questions.**

## Decision

### 1. Seam A — Injection: `plurInjector` as a `ContextTransformer`

1. **Construction & registration.** `plurInjector` is a
   `sessctx.ContextTransformer` constructed by the DI composition root with
   the `tools.MCPClient` for `MEMORY.SERVER`, and registered in
   `sessctx.Factory.Extras` at `initComponents`. **Priority 15** — after
   skills (10), before gatekeeper (80). The injector is token-counted and can
   trigger summarization of unpinned history. Distinct priorities are
   mandatory: pipeline ordering uses the non-stable `sort.Slice` (ADR-036), so
   priority 15 must not collide with skills' 10 or gatekeeper's 80.
2. **Marker-keyed replace-in-place, never append.** The `## PLUR MEMORY` /
   `[/PLUR-MEMORY]` sentinels delimit one self-delimited Part. On each turn
   the Part's `Text` is **replaced**; it is appended only on first injection.
   At most one memory block exists at any time (`memory-single-block`). The
   injector **never sets `req.PersistHistory`**: when a sibling canonical
   transformer does, the *latest* block persists and is marker-identified and
   replaced on the next turn.
3. **Fully stateless transformer (Round 2 refinement — no `lastEnabled`
   tracking).** Strip-on-disable is **content-driven**: when
   `ENABLED == false`, scan for the sentinel; if present, remove the Part
   (and drop the system Content if left with zero Parts) and set
   `req.PersistHistory = true`; if absent, no-op. The transformer is
   idempotent and self-healing under multi-pass `Prepare` (a failed strip
   persists → next pass retries; after a persisted strip the sentinel is gone
   until re-enable re-introduces it).
4. **Fetch-per-turn.** Each turn issues `plur_inject_hybrid` with
   `{task: <current user prompt>, budget: INJECT_BUDGET, scope?}`. **No
   `session_id`** — it is unobtainable at Transform time, and cross-persona
   recall argues against session-scoping. **No v1 cache** — the eager stdio
   spawn makes every call a warm roundtrip. The `MEMORY.CACHE_TTL` = 60s
   task-fingerprint-keyed cache is a **measured fallback ONLY** (trigger: E2E
   p95 > ~100–150ms) and is **not implemented in v1**. Contract: *recall is
   the live per-turn relevance-gated query result; staleness is bounded only
   by the query, never indefinite.*
5. **Fail-open with strip semantics.** On any MCP error/timeout: log, strip
   the marker block **in memory only** (never `PersistHistory`), and return
   unchanged. Invariant: *inject current recall, or nothing — never stale
   recall.*
6. **Defensive trim.** The returned block is trimmed to `INJECT_BUDGET`
   because pinned engrams make server-side size non-deterministic.

### 2. Seam B — Learning: `plurHook` as a `TurnHook`

1. **Registered once at `initComponents`**, never per-Chat: `WithEngineHook`
   appends, so per-Chat registration would double-fire. Belt-and-suspenders
   turn-scoped dedupe on the last `SessionID` + `Index`.
2. **Detached bounded context for all hook MCP calls.** Every call uses
   `context.WithTimeout(context.Background(), 3*time.Second)` (bounded by
   `min(3s, server EffectiveTimeout)`), because `AfterTurn` is synchronous on
   `ExecuteTurn`'s return path and the turn's own ctx is dead on
   cancellation/timeout. User aborts (`err == context.Canceled` /
   `DeadlineExceeded`) are still captured as error episodes via the detached
   ctx.

   **Why the 3s bound is safe for the default tier (issue #1412).** The
   session-end `plur_learn_batch` payload carries `max_llm_calls: 0`, which
   disables server-side LLM dedup entirely (upstream-pinned:
   `maxLlmCalls: 0 → 0 LLM calls`, `packages/core/test/learn-batch-cap.test.ts`
   in plur-ai/plur), removing the dominant latency leg. The residual legs — a
   warm embedder (the per-turn injector keeps it warm), a warm stdio child,
   and the local cosine/ADD write — are normally well under 3s. **Revisit
   triggers** if this changes: (a) `LEARN: full` is enabled — `plur_learn`
   has no `max_llm_calls` parameter, so its gated dedup still LLM-calls
   inside the same bound; (b) multi-process flock contention shows up in
   practice (concurrent persona flushes consuming the detached window).
3. **Episode sourcing — three-way classification keyed on the hook's `err`
   argument** (never `Turn.State.LastError`, which is stale on the
   empty-response retry path):
   - (i) `Response != nil` (any `err`) → episode = this turn's response text
     + `Mode` + `SessionID` + timestamp; annotate `LastError` if set.
   - (ii) `Response == nil && err != nil` → episode =
     `{error, prompt: <last user message from PreparedHistory, else omitted>,
     mode, session, timestamp}`. **Never `GetLastModelTurn`** (it would
     return the previous turn's response). No learning from this branch.
     **Round 2 refinement:** skip the episode when `IsTransient(err)` is true
     (retry-exhaustion on a transient error — `errors.Is` unwraps the "max
     retries reached" wrap) and record the skip at debug level.
   - (iii) `Response == nil && err == nil` → `GetLastModelTurn` (**the only
     valid case** — provably this turn's: the phase loop has exited and no
     history writes occur between `AddContent` and `notifyAfterTurn`); skip
     if the retrieved response has no text parts (this also suppresses
     intermediate tool-iteration turns, which fire `AfterTurn` per
     `Engine.Run` iteration).
4. **Internal episode shape vs wire mapping (issue #1410).** The internal
   `episode` model `{text, error, prompt, mode, session, timestamp}` is a
   capture-side data structure, **not** a wire shape — the old contract
   conflated the two. `buffer.go` no longer carries JSON tags on `episode`
   (the struct is tag-free) and the old "wire contract" comment is removed;
   it was the seed of the circular test this issue fixes. Wire shapes are
   produced explicitly by mapping helpers so the internal model can never
   accidentally define the on-the-wire contract again:
   - **capture tier** — `buildCaptureSummary` renders the required `summary`
     with a **text-presence discriminator**: branches (i)/(iii) (text
     present, even with an error annotation) map to the response text
     bounded at `maxEpisodeBytes` (2000, rune-safe); branch (ii) (empty
     text) maps error-first — `"error: <Error>"` with `" | user: <Prompt>"`
     folded in only when it fits in the remaining budget; the error always
     survives. The payload is exactly `{summary, agent, session_id}`.
   - **batch/full tiers** — `engramPayload` `{statement, scope?, tags}`
     (`json:"statement"`, `json:"scope,omitempty"`, `json:"tags"`).

### 3. `LEARN` tiers — `off | capture | batch | full`

The tiers are **mutually exclusive** (`memory-learn-tier-exclusive`); the
default is `batch`:

`MEMORY.ENABLED` is the master switch for the whole integration: when
`false`, no injection and no learning occur regardless of the tier —
`AfterTurn` returns before tier dispatch and `FlushSession` drains without
writing; `LEARN` is consulted only when `ENABLED` is true. `LEARN: off`
additionally disables learning while injection may still run.

- `off` — nothing.
- `capture` — per-turn `plur_capture` with payload **exactly**
  `{summary, agent, session_id}` — three keys; `summary` required and always
  bounded at `maxEpisodeBytes` via the `buildCaptureSummary` fit rule
  (§2.4). `text`/`error`/`prompt` are **not** parameters — the internal
  `episode` model is not a wire shape.
- `batch` — episodes + **session-end `plur_learn_batch`** of a bounded
  per-session ring buffer (**~20 *learnable* turns** — skip-at-append drops
  empty/whitespace-text episodes, so error turns never occupy ring capacity
  and cannot evict learnable content), flushed via
  `defer plurHook.FlushSession()` in `Chat` (success and error). Local
  extraction, zero LLM cost. The batch payload is `{engrams: [{statement,
  scope?, tags}], max_llm_calls: 0}` — `max_llm_calls: 0` disables
  server-side LLM dedup (zero LLM cost; deterministic cosine/ADD path;
  upstream-pinned semantics, issue #1412). Episodes belong to the capture
  tier's timeline, not the batch. `scope` is native per-engram (from
  `MEMORY.SCOPE` — never silently dropped); the identity convention is
  `tags: ["session:<id>", "mode:<mode>"]`; there is **no top-level
  `session_id`** and the payload is **never `engrams: []`** (an empty drain
  is a no-op). `FlushSession` **claims** the buffered episodes under lock
  (snapshot AND remove, so a concurrent flush can never double-write the
  same episodes), issues the MCP call **outside** the lock, and on write
  failure **restores** the claimed episodes — retained for the next flush
  opportunity, with any ring-bound drops reported on the failure Warn
  (`retained`/`dropped` keys) — and deletes the entry only on success.
  Fail-open means log-and-move-on, never delete-then-fail (issue #1412).
  Retention is **in-process only**: a process exit after a failed flush
  still loses the buffer (accepted fail-open posture); cross-process
  durability (a spill/retry queue) is a recorded future concern, not
  implemented.
- `full` — episodes + **gated per-turn direct `plur_learn`** of
  `{statement, scope?, tags}` — **no `agent`** (not a real parameter; the
  server silently ignores it) and **no `session_id`** (an unstarted
  session's `session_id` risks scope mis-resolution; there are zero
  `plur_session_start` calls repo-wide) — plus the same session-end
  `plur_learn_batch` flush as `batch`. Signal = the **user message only**;
  matcher = correction *frames* (`please remember` / `note`, `from now on`,
  `stop <verb>`, `don't|do not|never|always <imperative>` — documented
  heuristic list); flood bounds = `MAX_LEARNS_PER_SESSION` = **3**
  (hot-reloadable) + per-session exact-match hash dedupe. Statement = the
  user's own words, trimmed, tagged `["session:<id>", "mode:<mode>"]`.

`plur_ingest` (LLM extraction) is **never auto-fired** — explicit ingestion
remains agent-mediated. Fail-open: memory errors are logged and ignored
(ADR-029 §5 posture).

### 4. Write-path concurrency (multi-persona)

Documented acceptance: **last-write-wins** for genuinely concurrent writers —
PLUR's single `~/.plur/` YAML store is atomic+fsync but **unlocked**, so the
lost-update window is real and shared across all personas that share `$HOME`
(episodes/tensions/config are lock-protected). Mitigations:

1. **Sequential-orchestration rule** codified here: orchestrated multi-agent
   flows (e.g. the Issue-to-PR loop) relay one send/retrieve at a time and
   never run agents in parallel, so tell-me-go writers are naturally
   sequential in practice.
2. **Best-effort advisory `flock`** on `~/.plur/.tmg-write.lock`, held
   **only across the write MCP calls** — non-blocking `LOCK_EX|LOCK_NB`,
   polled on a bounded budget (max ~200–500ms via an injected clock),
   fail-open (log + proceed unlocked). Uncreatable lock file → proceed
   unlocked. Unix-only (build tags, no-op on Windows). Serializes
   tell-me-go writers only.

### 5. Config surface

```yaml
MEMORY:
  ENABLED: false            # master switch for the whole memory integration (injection AND learning); default false (opt-in); hot-reloadable
  SERVER: "plur"            # key of the MCP_SERVERS entry backing the memory; session-fixed
  INJECT_BUDGET: 2000       # tokens for plur_inject_hybrid per turn; hot-reloadable
  LEARN: "batch"            # off | capture | batch | full; default batch; hot-reloadable
  SCOPE: ""                 # optional; precedence: override-if-set → .plur.yaml → one surfaced warning
  MAX_LEARNS_PER_SESSION: 3 # flood bound for `full`; hot-reloadable
  # CACHE_TTL: 60           # fallback ONLY (measured trigger p95 > 100–150ms); task-keyed, write-invalidated
```

- **Default disabled → zero behavior change** for existing users.
- **Enabled-but-absent-server → warn and disable (two stages):**
  (1) *static* — `MemoryConfig.validate()` (mirroring `MCPServerConfig.validate`)
  at config load and every hot-reload re-parse; (2) *dynamic* — if
  `mcpFactory.Build` skips the server (client construction/token failure),
  `GetMCPClient` returns `(nil, false)` and `initComponents` logs
  `memory_server_unavailable` and constructs the memory components with
  effective `ENABLED = false` (inert, stable DI shape). Plus a **nil-client
  runtime guard** in `Transform`/`AfterTurn` (hot-reload `ENABLED=true` with a
  DI-fixed nil client → log + fail-open no-op).
- **Hot-reload:** `ENABLED` / `LEARN` / `INJECT_BUDGET` /
  `MAX_LEARNS_PER_SESSION` ride the existing watcher →
  `configRefreshHook.OnPhaseTransition` → `applyConfig` path; `SERVER` is
  restart-level.

### 6. DI wiring (Round 2 — settled)

- `defaultToolchainFactory.BuildRegistry` **stashes the built `mcpClients`
  map** on the factory (not retained today).
- `ports.ChatterComposer` gains **`GetMCPClient(name string)
  (tools.MCPClient, bool)`**, implemented on `sessionDeps` by unwrapping
  `plugin.MCPServerDependency.Client`.
- `ports.ChatterConfig` gains the **memory server key** (threaded from
  `Config.Memory.Server` by the chat factory, exactly as ProviderName/Model/
  Mode are threaded) — a deterministic seam with no refresh-ordering coupling.
- `app.NewChatter` calls `deps.GetMCPClient(cfg.Memory.Server)` and threads
  the client via `agent.WithMemoryClient(client, memCfg)`; `initComponents`
  wires the injector into `Factory.Extras` and the hook into `NewEngine` opts.
- **Zero ADR-029 changes** — memory propagation is a pre-chain atomic refresh
  in `prepareRuntimeConfig` (which already runs before the three delegates),
  like Limits.
- **Hot-reload sharing:** `domain_config.ConfigWatcher` gains
  `GetMemoryConfig() domain_config.MemoryConfig`; `runtimeConfig` gains a
  `Memory` field; `prepareRuntimeConfig` stores it; `plurInjector`/`plurHook`
  read a shared `*atomic.Pointer[MemoryConfig]` lock-free per turn.

### 7. Trust class

The injected block is system-prompt-positioned, automatically-written,
external text — the same trust class as skill injection (ADR-005) **minus**
the install-approval gate. Bounded in v1 by: local-only store (`plur_sync` /
remote out of scope), `ENABLED` default false, `LEARN` gating, and the
relevance gate. Block header wording (verbatim):

> ## PLUR MEMORY — recalled from the local memory store (user-authored or learned from your own sessions); follow them unless they conflict with explicit user instructions.

### 8. Observability (Round 2 — settled; Round 3 — issue #1410)

- **In-process surface:** append `injected_engrams:<ids>` to
  `ContextRequest.Metadata.Warnings` — this survives to `Turn.State.Metadata`
  via `ContextRefiner`.
- **Black-box surface:** an **Info-level** log line with the ids — the E2E
  harness asserts on stdout/stderr.
- **Telemetry:** add a general `Warnings []string` field to
  `telemetry.TurnTrace` (settled: a general field, not `InjectedEngrams` —
  reusable by future transformers), populated from `ContextMetadata` at
  `finalizeTurnTrace`.
- **Debug-level record** for skipped error episodes (the `IsTransient` skips
  of §2) and per-write Debug diagnostics.
- **Write-failure surface (Round 3 correction).** The per-turn
  `memory_write_failures` Warnings-append is **dropped** — it was
  Transform-time-only and dead for the hook (`finalizeTurnTrace` and the
  `TraceEvent` publish run **before** `notifyAfterTurn`, so a hook-append
  never reaches `Turn.State.Metadata`). It is replaced by the
  **session-end aggregate + per-tool all-or-nothing dead-tool notice**,
  surfaced by the single top-of-Chat flush-then-read defer: turns.log via
  `SystemMessageEvent` is the best-effort primary (it can race the telemetry
  drain on ctx cancel); the synchronous stderr `Warn` is the asserted
  surface.
- **Live-leg posture (Round 3).** Live E2E legs run behind
  `-tags=e2e_live` (precedent: `-tags=arch`) and are **never in
  `make check-full`**. Environment preconditions (npx present, handshake OK)
  skip the leg; once past the precondition stage, persistence assertions
  **hard-fail on mismatch** — no log-and-pass past the precondition stage.
  The `memory_live_test.go` header carries this posture.
- **Deployment requirement (Round 3 — verified finding, issue #1410 / T7).**
  Deployments must set `MCP_SERVERS.<server>.ENV.PLUR_TOOL_PROFILE: "full"`:
  the real `@plur-ai/mcp` v0.18.0 server's default **"lean"** tool profile
  rejects direct calls to `plur_capture` / `plur_learn_batch` /
  `plur_inject_hybrid` ("not directly callable under the current tool
  profile"); the server's own hint names `PLUR_TOOL_PROFILE=full` as the
  direct-call surface. Under the default profile the integration's
  write/injection paths are dead — and, since this fix, that
  misconfiguration is now **caught and surfaced** by the dead-tool notice
  instead of failing silently.

### 9. Domain-model governance (Round 2 — settled)

The `Memory` entity + `MemoryLearnTier` enum + 4 invariants +
`memory-injection-fail-open` scenario were added to
`docs/domain-model/tell-me-go.modelith.yaml` in commit `b3d0296c` (already
landed, with the re-rendered `.md` committed together — `modelith-check`
green). `scripts/modelith-layers.sh` gained the `Memory` exception and the
`MemoryLearnTier` enum exclusion (also landed); `modelith-drift` is clean.
Go mapping: `MemoryConfig` → `internal/domain/config/memory_config.go`
(sibling of `MCPServerConfig`); `plurInjector`/`plurHook` → unexported
adapters in `internal/agent/memory`.

**Pre-existing priority collision (acknowledged, out of scope):**
`HistoryPruner` and `finalContextValidator` both sit at priority 110
(ADR-036 non-stable `sort.Slice`). This is a pre-existing condition — **not**
a new violation introduced by this ADR's priority 15, and explicitly out of
scope for this issue.

### 10. The corrections tables (24 rows)

#### Round 1 (on #1402, 9 premises)

| # | Premise | Finding (verified) | Committed outcome |
| --- | --- | --- | --- |
| 1 | Seam B sources `Turn.State.Response` | Nil on success (clear-after-append) | Three-way classification keyed on hook `err`; `GetLastModelTurn` only when `err == nil` |
| 2 | Seam A appends to system `Parts` | Pinned system message never summarised → unbounded accumulation | Marker-keyed replace-in-place @ priority 15; `memory-single-block` |
| 3 | Fail-open = return unchanged | Leaves stale recall in a pinned message | Strip-on-failure AND strip-on-disable; `ENABLED` hot-reloadable |
| 4 | Hook registered per-Chat | `WithEngineHook` appends → double-fire | Registered once at `initComponents` |
| 5 | "One memory-enabled persona per HOME" | Contradicts E2E; guts multi-persona value | Withdrawn → multi-persona + last-write-wins + advisory `flock` + sequential backstop |
| 6 | Per-session cache/TTL | Eager spawn amortizes cold start; cache serves task-mismatched recall | No v1 cache, fetch-per-turn; `CACHE_TTL` 60s as measured fallback only |
| 7 | Single-sentence E2E | Nondeterministic (relevance gate + no pin tool) | Two-legged E2E (below) |
| 8 | `session_id` on injection call | Unobtainable at Transform time | Dropped; additive `ContextRequest.SessionID` as v1.1 follow-up |
| 9 | `LEARN: full` gating unspecified | Mechanics/flood control undefined | Four-point gate + four-tier ladder, default `batch` |

#### Round 2 (on #1403, 5 refinements — all settled)

| # | Area | Finding (verified) | Committed outcome |
| --- | --- | --- | --- |
| 10 | Seam A state machine | Multi-pass `Prepare` breaks stateful transition tracking | Fully stateless, content-driven strip-on-disable (sentinel presence), self-healing; replace-or-append single sentinel-delimited Part |
| 11 | Seam B lifecycle | `AfterTurn` has no ctx, is synchronous on the turn's return path, fires per `Engine.Run` iteration; `errors.Is` unwraps retry wraps | Detached bounded ~3s ctx per MCP call; text-parts filter suppresses tool iterations; branch (ii) gains `!IsTransient(err)` + debug-level skip record |
| 12 | DI wiring | `ChatterComposer` has no MCP accessor; clients not retained after registration; `ChatterConfig` lacks server key | `GetMCPClient(name)` on `ChatterComposer` (factory stashes map); server key on `ChatterConfig`; nil-client runtime guard; two-stage fallback |
| 13 | Governance | `MemoryLearnTier` also caught by layers regex; ADR-068 filename/numbering | `Memory` exception + `MemoryLearnTier` enum exclusion; `docs/adr/2026-09-automatic-plur-memory-integration.md` indexed + linked in main README; pre-existing 110/110 collision acknowledged in ADR-068 |
| 14 | Observability | `TurnTrace` carries no warnings today | `ContextMetadata.Warnings` (in-process) + additive general `TurnTrace.Warnings []string` + Info-level log line for CLI legs; live E2E behind `-tags=e2e_live` |

#### Round 3 (on #1410, 8 findings — all verified)

| # | Finding (verified) | Committed outcome |
| --- | --- | --- |
| 15 | Three payload mismatches (`plur_capture` missing required `summary`; `plur_learn_batch` sent `episodes`/`session_id` instead of `engrams[]`; `plur_learn` sent unknown `agent`) | Wire contract aligned: `{summary, agent, session_id}` / `{statement, scope?, tags}` / `{engrams:[{statement, scope?, tags}]}` |
| 16 | Fully-silent failure class: the MCP adapter surfaces isError rejections as `ToolResult.Error` with a nil Go error (`stdio_client.go` three-way split) — no Warn ever fired | Detection contract: `err != nil || result.Error != nil` at all four call sites (three write sites + injector) |
| 17 | Seam A blindness: an isError rejection's `result.Text` is the server's error message | Injector strips on `result.Error` — never builds a recall block from error text |
| 18 | Skip-at-append + per-tool granularity | Ring buffer holds learnable turns only; `writeStats` keyed (session → tool → {failures, attempts}); per-tool dead trigger (`attempts ≥ 1 && failures == attempts`) |
| 19 | Failure surface | Session-end aggregate + per-tool dead-tool notice via the single top-of-Chat flush-then-read defer; turns.log best-effort / stderr asserted; Warnings-append dropped |
| 20 | Probe 1 (per-item `session_id` on `plur_learn_batch`) | **OUTCOME: keep-tags** — the server accepted the item but the `plur_history` `engram_created` event carries only `{type, scope}`; the per-item `session_id` does not round-trip → the `tags: ["session:<id>", "mode:<mode>"]` convention is retained |
| 21 | Probe 2 (old `{statement, agent}` shape on `plur_learn`) | **OUTCOME: silently-ignore** — the server accepts the unknown `agent` param and creates the engram (no isError) |
| 22 | `PLUR_TOOL_PROFILE` | The default "lean" profile rejects direct calls to the write/inject tools; deployments must set `PLUR_TOOL_PROFILE=full` in the MCP server's ENV (see §8) |

#### Round 4 (on #1412, 2 findings — both verified)

| # | Finding (verified) | Committed outcome |
| --- | --- | --- |
| 23 | `plur_learn_batch` latency exceeds the 3s detached bound → the session-end flush times out and the buffered episodes are silently lost (the buffer was deleted before the call) | Payload carries `max_llm_calls: 0` (kills the LLM-dedup leg, zero LLM cost); `FlushSession` claims under lock and restores on failure, reporting ring-bound drops |
| 24 | Timeout work proposed (configurable knob, constant raise 3s→30s) | **Deferred** — with `max_llm_calls: 0` the residual legs are well under 3s; revisit triggers recorded in §2.2 |

### 11. Failure/disable matrix

| Condition | Behavior |
| --- | --- |
| Server available | **Inject latest** recall (fetch-per-turn `plur_inject_hybrid`, trimmed to `INJECT_BUDGET`) |
| Error/timeout | **Strip, no persist** — log, remove the marker block in memory only (never `PersistHistory`), return unchanged (*inject current recall, or nothing — never stale recall*) |
| Disabled (`ENABLED == false`) | **Whole integration off.** Injection: one-shot persisted strip (remove the sentinel-delimited Part if present, drop the system Content if left with zero Parts, set `req.PersistHistory = true`; if absent, no-op). Learning: no `plur_capture`, no buffering (`AfterTurn` returns before tier dispatch), and `FlushSession` drains the stale buffer without writing — zero writes to the PLUR store |

### 12. v1.1 follow-ups (recorded, not deferred silently)

- Additive `ContextRequest.SessionID` — **only if** PLUR's relevance scoring
  measurably benefits from session context.
- Per-persona scope separation via `plur_rescope` / store entries — the
  concurrency remedy; note `plur_inject_hybrid` takes a single `scope`, so
  cross-scope injection needs one call per scope.
- If PLUR gains a pin capability: optional `pinned: true` flag on learn — a
  config nicety, not a blocker.

### 13. Out of scope (this issue)

- **Deployment-specific work** (e.g. Niffler templates/provisioning/
  per-deployment defaults) — this issue is tell-me-go core. Deployment configs
  (e.g. `ait-tmg/configs/butler.yaml`) carry the same misleading "master
  switch" comment and should be aligned in deployment-specific work — flagged
  here (this repo's own `configs/` carry no MEMORY block).
- PLUR sync/team-store integration (`plur_sync`, shared remotes) —
  configuration-level, not core.
- Cursor-style lean tool profiles or `plur_admin` tool curating.
- **Changing PLUR itself** — the MCP surface is consumed as-is; there is no
  pin tool in v1, so pinning is out-of-band (PLUR CLI / store edit),
  documented as an E2E setup step.
- Memory for the TUI/history-browser surfaces.

## Consequences

**Positive:** memory flows automatically — relevant engrams are injected into
the context before each turn and learnings/episodes are captured after each
turn, so multi-persona deployments sharing one `TELL_ME_HOME` get
"install once, memory works" without relying on agent self-discipline. The two
seams (`plurInjector` context transformer at priority 15, `plurHook` TurnHook)
are deterministic extension points on existing architecture. `ENABLED` defaults
to false, so existing users see **zero behavior change**. Fail-open guarantees
hold everywhere: injection failure strips and never persists stale recall, and
memory errors are logged and ignored (ADR-029 §5 posture).

**Negative:** the write path accepts **last-write-wins** for genuinely
concurrent writers; the advisory `flock` is best-effort and serializes
tell-me-go writers only. Per-turn fetch adds latency on the turn's critical
path (no v1 cache — `CACHE_TTL` is a measured fallback only). The injected
block is system-prompt-positioned, automatically-written external text — a
trust surface, bounded in v1 by local-only storage, opt-in `ENABLED`, `LEARN`
gating, and the relevance gate (minus the skill-injection install-approval
gate).

**Neutral:** the MCP client is retained after tool registration (factory stash
+ `GetMCPClient` accessor); `telemetry.TurnTrace` gains a general `Warnings
[]string` field reusable by future transformers; the pre-existing 110/110
priority collision (`HistoryPruner` / `finalContextValidator`) is acknowledged
but untouched; the v1.1 follow-ups (additive `ContextRequest.SessionID`,
per-persona scope separation, optional pin flag) are recorded, not deferred
silently.

## References

- ADR-067 — MCP client architecture (`docs/adr/2026-08-mcp-client-architecture.md`)
- ADR-005 — skill injection (`docs/adr/2026-01-skill-injection-architecture.md`)
- ADR-029 — fallible `Reconfigure` delegate chain / `configRefreshHook`
  (`docs/adr/2026-05-fallible-reconfigure-delegate-chain.md`)
- ADR-036 — test determinism standards, non-stable `sort.Slice`
  (`docs/adr/2026-05-test-determinism-standards.md`)
- ADR-021 — test doubles in `*test` sub-packages
  (`docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md`)
- ADR-040 — complete session subpackage extraction (the skills extraction;
  the earlier draft's "ADR-030" citation was wrong — ADR-030 is Release
  Branch Synchronization Policy)
  (`docs/adr/2026-05-session-extraction-completion.md`)
- Domain model: `Context`, `Config`, `Turn` entities; invariants
  `context-within-budget`, `context-pinned-preserved`,
  `history-persisted-after-turn` (`docs/domain-model/tell-me-go.modelith.yaml`)
- Code seams: `internal/agent/session/context/`,
  `internal/agent/skills/injector.go`,
  `internal/agent/orchestrator/{engine,engine_types,engine_phases}.go`,
  `internal/agent/agent.go` (`initComponents`, `prepareRuntimeConfig`,
  `applyConfig`), `internal/domain/ports/session.go` (`ChatterComposer`),
  `internal/domain/config/{watcher,mcp_config}.go`,
  `internal/domain/telemetry/trace.go`,
  `internal/domain/tools/mcp_client.go`,
  `internal/infrastructure/di/{container,toolchain_factory,mcp_factory}.go`,
  `internal/pkg/clock/clock.go`, `tests/e2e/`, `scripts/modelith-layers.sh`
- Grill rounds: [Round 1 transcript](https://gist.github.com/gosharplite/6af5e8d44cd3d7cfd765ff4be1a2537e) ·
  [Round 2 transcript](https://gist.github.com/gosharplite/213ada4b83cca349afeadef12ff729f0)
- Superseded issues: #1402, #1403
- Issue #1412 — latency-layer follow-on: `max_llm_calls: 0` + retain-on-failure
