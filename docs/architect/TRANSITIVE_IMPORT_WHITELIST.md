# Transitive Import Whitelist — ADR-056, Decision 2

Architect-curated. The architect owns whitelist maintenance: every consumer-level
closure edge in this list is a recorded decision, and no entry is added, removed,
or edited except by the architect. The transitive-closure gate reads this file at
verification time; since the 2026-08 ratification the gate is STRICT (failing):
new closure growth must be adjudicated. Family-level whitelists are deferred — no
schema exists for them yet; until one exists, only consumer-level entries are
recognized.

Closure semantics (ratified 2026-08): the gate measures the MERGED (prod+test)
compiled footprint — `go test` compiles prod+test binaries together, so the merged
closure is the real coupling surface. Test-edge excess is explicitly tagged with
`# P4` in the per-entry rationale.

Rationale legend:
- P1 = events→telemetry (recorded decision, domain/events/types.go)
- P2 = domain-internal hub (llm↔events↔tools cross-edges, legal domain→domain)
- P3 = infra→domain dependency-inversion reach
- P4 = test-double coupling (merged closure)
- P5 = services via domain/tools (WorkspacePolicy leg)

## decision: events → telemetry

First recorded decision: the `events` family legitimately depends on `telemetry`
(`internal/domain/events/types.go` → `internal/domain/telemetry`), so a consumer
whose closure reaches `events` is justified in also reaching `telemetry`. This
edge is why the derived constant spans 9 families.

## consumer: cmd/deadcode
allowed: events, llm, persistence, security, services, telemetry, tools
# P2+P1: analysis/security toolchain surface; telemetry via events (recorded decision)
## consumer: cmd/tell-me-go
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P1+P2+P5: full hub surface via agent/analysis/cli deps
## consumer: internal/agent
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P5+P1: services via tools; telemetry via events
## consumer: internal/agent/agentinternal
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# test-access bridge mirrors agent's domain surface
## consumer: internal/agent/agenttest
allowed: config, events, llm, persistence, pricing, security, skills, telemetry, tools
# P1+P4: canonical test-double surface; telemetry via events; config for StubChatterComposer
## consumer: internal/agent/orchestrator
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P2+P5: engine/session deps
## consumer: internal/agent/orchestrator/orchestratortest
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P4: test harness mirrors orchestrator surface
## consumer: internal/agent/session
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P1+P2+P5: lifecycle deps
## consumer: internal/agent/session/context
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# AMENDED: config prod (strategy/gatekeeper); persistence/pricing/security/services/skills test-edge (P4)
## consumer: internal/agent/session/ui
allowed: config, events, llm, persistence, pricing, security, skills, telemetry, tools
# P1+P2+P4: UI subpackage deps; config via agenttest (P4)
## consumer: internal/agent/skills
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P1+P2+P5: skill-injection deps
## consumer: internal/cli
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P1+P2+P5: CLI command deps
## consumer: internal/cli/clitest
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P4: test package mirroring CLI surface
## consumer: internal/domain/config
allowed: events, llm, pricing, telemetry, tools
# P1+P2: llm/telemetry/tools via events
## consumer: internal/domain/config/configtest
allowed: config, events, llm, pricing, telemetry, tools
# P4: test package
## consumer: internal/domain/events/eventstest
allowed: events, llm, telemetry, tools
# P1+P2: events' own surface
## consumer: internal/domain/pricing
allowed: llm, tools
# P2: tools via llm→events→tools
## consumer: internal/infrastructure/auth
allowed: persistence, services
# P3: via infrastructure/persistence (imports domain/persistence + domain/services)
## consumer: internal/infrastructure/config
allowed: config, events, llm, pricing, telemetry, tools
# P1+P2: via config+events deps
## consumer: internal/infrastructure/di
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# P1+P3: composition root; telemetry via events
## consumer: internal/infrastructure/history
allowed: llm, persistence, services, tools
# P2+P3+P5: domain deps
## consumer: internal/infrastructure/llm
allowed: config, events, llm, persistence, pricing, services, telemetry, tools
# P1+P3: adapter deps
## consumer: internal/infrastructure/llm/anthropic
allowed: llm, persistence, services, tools
# P3: adapter deps
## consumer: internal/infrastructure/llm/gemini
allowed: events, llm, persistence, services, telemetry, tools
# P1+P3: adapter deps
## consumer: internal/infrastructure/llm/llmerr
allowed: llm, tools
# P2: tools via llm
## consumer: internal/infrastructure/llm/openai
allowed: llm, persistence, services, tools
# P3: adapter deps
## consumer: internal/infrastructure/logging
allowed: events, llm, persistence, services, telemetry, tools
# P1+P2+P3: via events/llm deps
## consumer: internal/infrastructure/telemetry
allowed: config, events, llm, pricing, security, telemetry, tools
# P2+P3: config via events/telemetry deps
## consumer: internal/tools
allowed: events, llm, persistence, pricing, security, services, telemetry, tools
# P1: telemetry via events
## consumer: internal/tools/analysis
allowed: events, llm, persistence, security, services, telemetry, tools
# P1+P2: llm/telemetry via events
## consumer: internal/tools/analysis/analysistest
allowed: events, llm, persistence, security, services, telemetry, tools
# P4: test package
## consumer: internal/tools/developer
allowed: events, llm, persistence, security, services, telemetry, tools
# P1+P2: llm/telemetry
## consumer: internal/tools/integrations/ado
allowed: llm, persistence, security, tools
# P2+P3: tool deps
## consumer: internal/tools/integrations/atlassian
allowed: llm, persistence, security, tools
# P2+P3: tool deps
## consumer: internal/tools/workspace
allowed: events, llm, persistence, security, services, telemetry, tools
# P1+P2: llm/telemetry
## consumer: internal/ui
allowed: config, events, llm, persistence, pricing, security, services, telemetry, tools
# P1+P2+P5: UI deps
## consumer: internal/ui/tui
allowed: llm, security, tools
# AMENDED: security prod (prompt_capturer UserInteractor); tools test-edge (P4)
## consumer: internal/ui/tui/progress
allowed: config, events, llm, persistence, pricing, security, services, telemetry, tools
# P1+P2+P5: progress deps
## consumer: internal/pkg/metricsfmt
allowed: events, llm, telemetry, tools
# events=prod-direct; llm=test-direct (P4, white-box test builds llm.Metrics); telemetry=P1 (events→telemetry); tools=P2 (events→llm→tools)

## consumer: internal/domain/ports
allowed: <derived>

## consumer: internal/app
allowed: config, events, llm, persistence, pricing, security, services, skills, telemetry, tools
# TD-1 (issue #1364): app construction seam (chatter/chat-service/suggestions); full hub surface

## infra-root: di
allowed: config, exec, factory, history, llm, logging, mcp, persistence, registry, security, skills, telemetry, toolchain
# infra-lateral composition: di composes infra adapters (config added for P1 config watcher injection — issue #1369; mcp added for the MCP client factory — issue #1373); no application-layer imports (ADR-066)

## infra-sanctioned: llm → auth
# llm/factory.go constructs auth (issue #1350 item 4, ADR-055 confinement)

## infra-sanctioned: auth → persistence
# auth/default_fs.go (issue #1350 item 3)

## infra-sanctioned: history → persistence
# history/default_asset_store.go (issue #1350 item 5)
