# Transitive Import Whitelist — ADR-056, Decision 2

Architect-curated. The architect owns whitelist maintenance: every consumer-level
closure edge in this list is a recorded decision, and no entry is added, removed,
or edited except by the architect. The transitive-closure gate (v1, report-only)
reads this file at verification time. Family-level whitelists are deferred — no
schema exists for them yet; until one exists, only consumer-level entries are
recognized.

## decision: events → telemetry

First recorded decision: the `events` family legitimately depends on `telemetry`
(`internal/domain/events/types.go` → `internal/domain/telemetry`), so a consumer
whose closure reaches `events` is justified in also reaching `telemetry`. This
edge is why the derived constant spans 9 families.

## consumer: internal/ui/tui
allowed: llm

## consumer: internal/agent/session/context
allowed: llm, tools, events, telemetry

## consumer: internal/domain/ports
allowed: <derived>
