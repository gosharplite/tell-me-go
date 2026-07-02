# ADR-046: Remove `ask_user` Tool — Turn Boundaries Replace Mid-Turn Prompts

- **Status:** Accepted
- **Date:** 2026-07
- **Author:** Architect (tell-me-go)

## Context

The agent tool registry included an `ask_user` tool (`internal/tools/workspace/interaction.go`) that paused execution mid-turn, prompted the user via the terminal, and fed the response back into the same tool-call loop. It was used in SOP documentation as the escalation mechanism when the Orchestrator needed human input (issue number confirmation, version approval, dispute resolution, error reporting).

The tool was evaluated against three findings:

1. **No efficiency benefit.** Every tool call already sends the full history to the LLM. Whether the model outputs text (ending the turn) or calls `ask_user` (continuing the loop), the next API call carries the same context payload. There is no "mid-turn" savings.

2. **Broken in piped multi-agent workflows.** When an SOP orchestrator pipes output between agents, if a sub-agent calls `ask_user`, it receives EOF — there is no human on the other end of the pipe. The pipeline breaks silently. The natural turn boundary (model outputs question → loop ends → Orchestrator reads response) works identically in interactive and piped contexts.

3. **Worse failure mode.** At a turn boundary, the model stops and waits cleanly. With `ask_user`, if the human doesn't respond before the tool times out (or EOF is received), the model receives an empty/error result and begins guessing. This path does not exist with turn-based interaction.

## Decision

**Remove `ask_user` from the tool registry.** The correct pattern for soliciting human input is: the model outputs its question as a text response, the agent loop ends the turn, and the human answers on the next turn. This is clean, predictable, and works identically in interactive and piped multi-agent contexts.

## Changes

| Area | File | Action |
|------|------|--------|
| Tool registration | `internal/tools/workspace/registration.go` | Remove `ask_user` registration block and `interaction` variable |
| Dead code | `internal/tools/workspace/interaction.go` | Delete entire file |
| Dead tests | `internal/tools/workspace/interaction_test.go` | Delete entire file |
| Security policy | `internal/domain/security/policy.go` | Remove `"ask_user": true` from `AllowedCommands` |
| Registration tests | `internal/tools/workspace/registration_test.go` | Remove `ask_user`-specific subtest and references |
| SOP: agentic capabilities | `docs/sop/agent/agentic_capabilities.md` | Remove `ask_user` from interactive tools example |
| SOP: public release | `docs/sop/lifecycle/public_release.md` | Replace `ask_user` instruction with turn-boundary pattern |
| SOP: PR review | `docs/sop/lifecycle/pr_review.md` | Replace all 5 `ask_user` references with turn-boundary pattern |
| SOP: issue-to-PR | `docs/sop/lifecycle/issue_to_pr_orchestration.md` | Replace all 6 `ask_user` references with turn-boundary pattern |

## Consequences

### Positive

- **Unified interaction model.** Interactive and piped workflows use the same mechanism: model outputs question, loop ends, human (or Orchestrator) responds next turn.
- **No more silent EOF in pipelines.** Sub-agents cannot stall waiting for human input that will never arrive.
- **Simpler code.** One less tool, one less source file, one less test file, one less entry in the security policy.
- **No guessing path.** Eliminates the timeout/EOF → empty result → model hallucinates failure mode.

### Negative

- **SOP documents now describe the pattern rather than referencing a tool name.** This is slightly more verbose but also more explicit — the documents now teach the *why*, not just the *what*.
- **No programmatic hook for "the model wants user input."** Orchestrators must detect this from model text output rather than a tool call. In practice, SOP orchestrators already read model output text to route responses; this is not a regression.

## References

- [ADR-002: Tool Execution Concurrency and Timeouts](../adr/2026-01-tool-execution-concurrency-and-timeouts.md) — `LongRunning` timeout exemption originally designed for `ask_user`
- [Issue-to-PR Orchestration SOP](../sop/lifecycle/issue_to_pr_orchestration.md)
- [PR Review SOP](../sop/lifecycle/pr_review.md)
