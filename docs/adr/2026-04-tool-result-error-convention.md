<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-022: Tool-Result Error Convention

## Status

Accepted

## Context

`tell-me-go` exposes ~70+ tool handlers to the LLM via a single registry
(`internal/infrastructure/registry`). Every handler conforms to the same
signature, defined in `internal/domain/tools/types.go`:

```go
type ToolFunc func(ctx context.Context, args map[string]interface{},
    hb chan<- struct{}) (ToolResult, error)

type ToolResult struct {
    Text       string
    BinaryData []BinaryData
    Error      error                  // optional: captures error during execution
    Metadata   map[string]interface{} // structured data passed to orchestrator
}
```

The signature offers **two channels** for signalling failure:

1. The Go return value `error`.
2. The `Error` field of `ToolResult`, plus the implicit channel of writing
   an error message into `ToolResult.Text` (the "model-friendly" channel,
   which is what the LLM actually sees as the tool's output in its next
   turn).

The codebase has never written down which channel SHOULD be used for which
failure mode. An empirical survey of five representative handlers, plus
the registry choke-point and the LLM provider response handlers, finds
that all three patterns coexist without rule:

| Handler                                     | Missing required arg            | Runtime / system failure | Domain failure (e.g. no rows) |
|--------------------------------------------|---------------------------------|--------------------------|-------------------------------|
| `analysis.generateMermaidDiagram`           | `ToolResult{Text:"Error: ..."}, nil` | n/a                      | `Text:"Error: ..."`, nil      |
| `workspace.gitManager.gitCommit`            | Returns Go `error`               | Returns Go `error` + partial `Text` | Returns Go `error` with actionable message |
| `analysis.healthManager.GetCodeHealth`      | n/a                              | Embeds "ERROR" status in formatted `Text` | Embeds status in `Text` |
| `integrations.adoManager.adoListPipelineRuns` | Returns Go `error`             | Returns Go `error`, empty `ToolResult{}` | Empty Text result, nil error  |
| `workspace.diagnosticTool.checkSystemHealth`  | n/a                            | Returns Go `error`, empty `ToolResult{}` | n/a                           |

The `ToolResult.Error` field, despite being declared, is **never written
to** by any handler. Failure information always flows through one of:
the Go return error, the `Text` body, or both.

Two recent series exposed the cost of this drift:

* The **central required-args guard** added to `registry.Execute` (commit
  `ce97fd79`) had to pick a single convention for the 70+ handlers it
  intercepts. It chose `ToolResult{Text:"Error: ..."}, nil` to match
  `generateMermaidDiagram`, explicitly because the model can react to a
  text body in the same turn but treats a Go `error` return as a turn
  abort. That choice was correct *for missing-args* but was made in
  isolation, with no policy stating when other handlers SHOULD follow
  the same rule.

* The **LLM provider truncation series** (commits `5031162c` for
  Anthropic and OpenAI; `0495c6a3` for Gemini) hit the inverse failure
  mode. When a provider hit `MaxTokens`/`MaxOutputTokens` mid-tool-call,
  the resulting partial JSON args reached `registry.Execute`, which
  emitted a model-friendly `Text:"Error: missing required parameters
  [content reason]"` with nil Go error. The model interpreted this as
  *its own* malformed call and looped, retrying the same payload and
  burning real LLM dollars per retry. The fix was to make truncation
  surface as a Go `error` from `fromAnthropicResponse` /
  `processResponse`, aborting the turn instead of pretending it was a
  recoverable in-turn signal.

The two series, taken together, show a real distinction:

* Some failures (missing args, invalid filter regex, "no matches",
  user-cancelled confirmation, "pipeline already exists") are **domain
  outcomes** the model can correct or react to within the same
  conversation turn.
* Other failures (provider-side truncation, infrastructure outage,
  context cancelled, security policy violation, panic-equivalent) are
  **infrastructure faults** that the model cannot fix by retrying — the
  turn must abort and the orchestrator must take over.

Without a written rule, every new tool author re-derives this
distinction (often incorrectly) and existing tools surface failures
inconsistently. The `cli_standards.md` guidance ("If a state-management
tool returns an error … this MUST be treated as a blocking failure") is
the only existing precedent and it presumes the convention this ADR now
codifies.

## Decision

Project tools MUST signal failure using a **hybrid convention** that
distinguishes infrastructure faults from domain-level outcomes by the
channel they use:

* **Infrastructure / system faults MUST be returned as a non-nil Go
  `error`.** The `ToolResult` value MAY be empty (`tools.ToolResult{}`)
  or MAY carry partial output for diagnostic purposes. `registry.Execute`
  wraps any non-nil handler error as `tool execution failed: NAME: ERR`,
  and the orchestrator MUST treat this as a turn-aborting signal. The
  LLM does NOT see the wrapped error as in-turn tool output — it sees a
  failed-turn event.

* **Domain-level outcomes MUST be returned as `tools.ToolResult{Text:
  "..."}, nil`.** The `Text` body is what the LLM consumes in its next
  turn. A leading `Error:` prefix SHOULD be used when the outcome
  represents a corrective failure the model is expected to react to
  (missing parameter the model can supply on retry, invalid input the
  model can reformulate, "no matches found" the model can broaden). The
  Go return error MUST be `nil` in this path — the turn is not aborted.

* **The `ToolResult.Error` field is reserved.** Handlers SHOULD NOT
  populate it. It exists for future structured-error propagation (see
  Alternatives Considered) but is currently dead code in every handler
  surveyed. Tooling that consumes `ToolResult` MUST NOT depend on it
  being populated.

Concrete classification rules tool authors apply mechanically:

1. **Missing or malformed required parameter** → domain outcome.
   Return `ToolResult{Text: "Error: ..."}, nil`. The central
   `registry.Execute` guard already implements this for keys listed in
   `Schema.Required`; per-handler validation of value shape (e.g. "must
   be a positive integer") MUST follow the same pattern.

2. **External system returned an actionable error the model can fix**
   (e.g., git "nothing to commit", ADO "pipeline already exists",
   user-cancelled confirmation) → domain outcome. Return `ToolResult{
   Text: "..."}, nil` with a description the model can act on. The Go
   return error MUST be `nil`. **Note:** the existing `gitCommit`
   handler returns both `Text` and a Go `error`; this dual-write
   predates this ADR and is preserved as-is for UX reasons (see
   Consequences §Negative). New handlers MUST NOT replicate that
   pattern.

3. **Network/transport failure, decode failure, file-system error,
   security-policy violation, ctx.Done, panic-recovered failure,
   provider-side truncation, internal invariant violation** →
   infrastructure fault. Return `tools.ToolResult{}, fmt.Errorf("...:
   %w", err)`.

4. **Partial success with a known truncation boundary** (e.g.,
   `getGitCommit` truncating to 300 lines, ADO log filtering hitting
   `maxLines`) → domain outcome with explicit annotation. Return
   `ToolResult{Text: body + "\n... (Output truncated) ..."}, nil`. The
   model is informed and may request narrower output. This pattern is
   already in use and is preserved.

5. **Empty-but-successful result** (e.g., "no pipelines found", "no
   matches found", `git status` clean) → domain outcome. Return
   `ToolResult{Text: "..."}, nil` with a body that explicitly states
   the empty outcome. Handlers MUST NOT return `ToolResult{}, nil` for
   a successful empty result — an empty `Text` is indistinguishable from
   a swallowed failure to downstream consumers.

These rules apply at the **handler boundary**. Internal helpers a
handler calls into are free to return Go errors freely; the handler is
responsible for translating them at the boundary per the rules above.

## Consequences

### Positive

* **A single rule operators can apply.** New tool authors no longer
  re-derive the channel choice from first principles. Reviewers no
  longer have to argue case-by-case in PR review whether a given
  failure should be a Go `error` or a `Text` body.

* **The model can reason about its own failures.** Domain outcomes flow
  through `Text`, which the LLM sees in its next turn and can act on.
  This is what made the central required-args guard work: the model
  reads "Error: missing required parameter \"path\"" and supplies it on
  the retry. The same reasoning applies to "no matches found",
  "pipeline already exists", and similar.

* **Infrastructure faults abort the turn correctly.** Truncation at
  `MaxTokens`, security violations, and context cancellations now have
  a uniform channel that bypasses the model's reasoning loop. This
  eliminates the loop-on-retry failure mode that motivated the LLM
  truncation series — the model never sees the truncation as a "tool
  output it should react to".

* **Aligns with the existing registry choke-point.** The central
  `validateRequiredArgs` guard already follows Convention C semantics
  (model-friendly text for what it intercepts; the wrapping
  `"tool execution failed: NAME: ERR"` for everything else). This ADR
  ratifies what the registry already does, rather than inventing a new
  contract.

* **`cli_standards.md` becomes mechanically enforceable.** The
  state-management tool blocking-failure rule depends on being able to
  tell "real error" from "in-turn outcome". With this ADR's contract,
  a non-nil Go `error` IS the blocking signal, unambiguously.

### Negative

* **Migration burden on existing non-compliant tools.** Of the five
  representative handlers surveyed, only `git.gitCommit` cleanly
  follows the hybrid contract. The integration tools (ADO) overuse
  Convention A — they return Go `error` for cases like "pipeline
  already exists" that are domain outcomes, which currently abort the
  turn and force the model to a fresh call instead of letting it react
  in-turn. The analysis tools (`generateMermaidDiagram`,
  `GetCodeHealth.GetDetailedCoverage`) overuse Convention B — they
  return `ToolResult{Text: "Error: ..."}, nil` for what are arguably
  infrastructure faults (missing dependency, decode failure). Bringing
  every handler into compliance is a substantial follow-up effort
  tracked separately as **Task G-Followup** (backlog only; not yet
  filed). Until that effort lands, the codebase remains heterogeneous
  and the ADR is aspirational for non-compliant handlers.

* **Requires an enforcement mechanism that does not yet exist.** This
  ADR is text. Nothing structural prevents a future handler from
  picking the wrong channel. The `ToolResult.Error` field remains
  declared-but-unused, which itself invites confusion (see Alternatives
  Considered §3 for why we did not eliminate it). A linter rule, a
  registry-level audit pass, or a new handler-signature wrapper type
  would all enforce the convention more strongly; none is in scope
  here. The Task G-Followup effort SHOULD include consideration of
  a structural enforcement mechanism, not only per-handler edits.

* **Hybrid handlers like `gitCommit` are explicitly tolerated as
  legacy.** The decision text says new handlers SHOULD pick one
  channel, but the existing dual-write in `gitCommit` (returns BOTH
  `Text` AND a Go `error`) is preserved because the corrective error
  message is genuinely useful and removing it would degrade UX. This
  creates a "do as I say, not as I do" pattern that future maintainers
  may legitimately point at when defending similar dual-writes. The
  Task G-Followup audit MUST decide whether the gitCommit pattern is a
  third allowed channel or a wart to clean up.

* **The `ToolResult.Error` field becomes deliberately dead.** Until a
  future ADR re-purposes it (e.g., for typed structured errors carrying
  `errors.Is`/`errors.As` discrimination), it is documented dead code.
  The dead-code analyzer SHOULD be configured to ignore this specific
  field, or the field SHOULD be deleted in a future refactor. Neither
  is decided here.

## Alternatives Considered

* **Convention A: All failures return a Go `error`; `ToolResult` is
  `nil` or empty on failure.** Rejected. This is the simplest rule and
  matches the standard Go idiom for non-tool functions, but it
  collapses the in-turn / turn-abort distinction the truncation series
  proved to be load-bearing. Under Convention A, the central
  required-args guard cannot tell the model "you forgot the `path`
  argument" without also aborting the turn — and that is exactly the
  failure mode commit `ce97fd79` was structured to avoid (the pre-ce97
  per-handler guards that returned Go errors caused the model to abort
  rather than self-correct, which is why the choke-point was changed
  to use the `Text` channel).

* **Convention B: All failures return `ToolResult{Text: "Error: ..."},
  nil`; the Go `error` channel is used only for unrecoverable internal
  panics.** Rejected. Convention B is what `generateMermaidDiagram` and
  the `GetDetailedCoverage` handler currently do, and it has the
  inverse failure mode of Convention A: provider-side truncation,
  security violations, and context cancellations would all be presented
  to the model as in-turn outcomes it could react to, which is exactly
  what caused the looping failure mode commits `5031162c` and
  `0495c6a3` had to fix at the provider boundary. Convention B also
  makes `cli_standards.md`'s blocking-failure rule mechanically
  unenforceable — there is no way to tell a "real" tool failure from a
  domain outcome at the orchestrator level if both flow through
  `Text`.

* **Use the `ToolResult.Error` field as the canonical failure channel,
  separate from both `Text` and the Go return error.** Rejected. The
  field is currently dead code in every handler surveyed, so adopting
  it would require a 70+ handler migration with zero existing
  precedent. Worse, it would create a *third* failure channel and
  complicate, rather than simplify, the contract. If a future ADR finds
  value in structured typed errors (e.g., to let consumers
  `errors.Is(res.Error, ErrSecurityViolation)` from the result struct),
  that ADR can re-purpose this field with a fresh contract; this ADR
  deliberately does not.

* **Make `ToolFunc` return only `ToolResult` and embed all failure
  classification in a structured `ToolResult.Status` enum.** Rejected.
  This would require a signature change across 70+ handlers, every
  registry implementation, every test mock (`MockToolRegistry`,
  `PanicRegistry`, the integration test mocks under
  `tests/integration/agent/...`), and every LLM provider's tool-result
  serializer. The migration cost is order-of-magnitude larger than the
  benefit, and the same in-turn / turn-abort distinction can be
  expressed with the existing two-channel signature by following this
  ADR's rules.

## References

* Implementing source files
  * `internal/domain/tools/types.go` — `ToolResult`, `ToolFunc`,
    `Registry` interface definitions.
  * `internal/infrastructure/registry/registry.go` — `Execute`,
    `validateRequiredArgs`. The choke-point that wraps non-nil handler
    errors as `tool execution failed: NAME: ERR` and that emits
    domain-outcome `Text: "Error: ..."` for missing required args.
  * `internal/infrastructure/registry/required_args_guard_test.go` —
    pins the model-friendly behaviour of the central guard.
  * `internal/tools/analysis/mermaid_tool.go` — reference for
    Convention B (the "Error:" text body pattern) used by the central
    guard.
  * `internal/tools/workspace/git.go` — reference for the hybrid
    channel; `gitCommit` is the closest existing handler to the new
    contract.
  * `internal/tools/workspace/diagnostic_tools.go` — reference for
    Convention A (Go `error` for infrastructure faults) on the
    `check_system_health` path.
  * `internal/infrastructure/llm/anthropic/client.go` — `checkTruncation`
    surfaces `stop_reason="max_tokens"` as a Go error so the turn
    aborts.
  * `internal/infrastructure/llm/gemini/gemini.go` — `checkResponse`
    surfaces `FinishReason==MAX_TOKENS` as a Go error for the same
    reason.

* Motivating commits
  * `ce97fd79` — `tools(registry): centralize required-parameter
    validation in registry.Execute`. Establishes that the central guard
    uses `Text:"Error: ..."` precisely because the model can recover
    within the turn; per-handler Go-error guards were the inverse
    failure.
  * `5031162c` — `llm(anthropic,openai): fix silent tool-call truncation
    at max_tokens`. Establishes that provider-side truncation MUST
    surface as a Go error, not as an in-turn `Text` body, because the
    inverse caused the agent to loop.
  * `0495c6a3` — `llm(gemini): fix silent tool-call truncation at
    MaxOutputTokens`. Same root cause as `5031162c`, applied to the
    Gemini provider.

* Related ADRs
  * **ADR-002** — Standardize Tool Execution Concurrency, Timeouts, and
    Context Propagation. Adjacent contract: defines when a tool MAY be
    cancelled. Cancellation surfaces here as an infrastructure fault
    (Go `error`).
  * **ADR-012** — Dynamic Tool Discovery via Capability Toolkits. The
    discovery layer enumerates tools but does not change their failure
    contract; this ADR is orthogonal.
  * **ADR-014** — Event-Driven Orchestration and Circuit Breaker
    Pipeline. The orchestrator is the consumer that distinguishes
    "abort turn" (Go `error`) from "feed back to model" (`Text`); the
    circuit breaker counts the former, not the latter.

* Related SOPs
  * `docs/sop/standards/cli_standards.md` § "Tool Error Handling" —
    pre-existing rule that "if a state-management tool returns an
    error, this MUST be treated as a blocking failure". This ADR makes
    that rule mechanically applicable by defining what "returns an
    error" means: a non-nil Go `error` from the handler. Domain
    outcomes flowing through `Text` are explicitly NOT blocking.

* Forward references (NOT yet filed)
  * **Task G-Followup** — Audit and migrate non-compliant tools.
    Backlog only at the time of this ADR. Scope: walk every handler in
    `internal/tools/...`, classify against §Decision rules 1–5, and
    propose per-handler fixes. SHOULD also evaluate a structural
    enforcement mechanism (linter, registry-level audit, or
    handler-signature wrapper) so that a future regression cannot
    silently re-introduce the inconsistency this ADR was written to
    eliminate.
