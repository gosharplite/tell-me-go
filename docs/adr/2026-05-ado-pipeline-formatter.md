# ADR: Pipeline Presentation Extraction via PipelineFormatter Interface

**Status:** Accepted  
**Date:** 2026-05-01  
**Supersedes:** N/A  
**Issue:** #112 Phase 4

## Context

`internal/tools/integrations/ado/pipeline.go` (991 LOC) violates Hexagonal Architecture by embedding presentation logic inside an infrastructure adapter. Specifically:

- `formatPipelineRunsList` — renders `[]adoPipelineRun` into a human-readable `string` with markdown-like formatting and `fmt.Fprintf` calls
- `formatBranchRef` — applies domain-agnostic branch-name heuristics (vX.Y.Z → `refs/tags/`, else → `refs/heads/`)
- Inline `fmt.Fprintf` calls within `AdoGetPipelineRun`, `AdoListPipelines`, etc. — formatting output for LLM consumption

These presentation functions live in `package ado` alongside HTTP transport, JSON marshaling, URL construction, and authentication. This is a layering violation: the Infrastructure/Transport layer should return raw domain structs; the Presentation layer should format them.

## Decision

We adopt **Strategy B** from Issue #112: extract a `PipelineFormatter` interface at the consumer side, keeping the ADO client a pure infrastructure adapter.

### Strategy B Design

1. **Define `PipelineFormatter` interface** in the consumer package (currently `internal/tools/integrations/ado` — the tool registration layer). The interface defines formatting methods that the ADO client *does not* implement:

```go
// PipelineFormatter formats ADO domain types for display.
// The ADO client returns raw structs; formatting belongs to the consumer.
type PipelineFormatter interface {
    FormatPipelineRunsList(pipelineID int, runs []adoPipelineRun) string
    FormatPipelineRunDetail(run *adoPipelineRunDetail) string
    FormatPipelineList(pipelines []adoPipeline) string
    FormatBranchRef(branch string) string
}
```

2. **Move formatting implementations** into a new file `pipeline_format.go` in the same `package ado` (not a sub-package). This keeps them co-located with the types they format, but **the ADO client struct (`AdoManager`) does not own them**. They become package-level functions or methods on a new `pipelineFormatter` struct.

3. **Refactor `AdoManager` tool methods** to return raw structs. The formatting call is made by the tool registration closure in `RegisterTools` or equivalent, not by the ADO client methods themselves.

**Current (layering violation):**

```go
// internal/tools/integrations/ado/pipeline.go
func (m *AdoManager) AdoListPipelineRuns(...) (tools.ToolResult, error) {
    // ... HTTP fetch ...
    return tools.ToolResult{Text: m.formatPipelineRunsList(params.PipelineId, runs)}, nil
}
```

**Scalable (Strategy B):**

```go
// internal/tools/integrations/ado/pipeline.go
func (m *AdoManager) ListPipelineRuns(...) ([]adoPipelineRun, error) {
    // ... HTTP fetch ...
    return runs, nil
}

// internal/tools/integrations/ado/pipeline_format.go
func (f *pipelineFormatter) FormatPipelineRunsList(pipelineID int, runs []adoPipelineRun) string { ... }

// internal/tools/integrations/ado/register.go (tool registration)
r.RegisterWithOptions(..., func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
    runs, err := m.ListPipelineRuns(ctx, params)
    if err != nil { return tools.ToolResult{}, err }
    return tools.ToolResult{Text: formatter.FormatPipelineRunsList(params.PipelineId, runs)}, nil
}, ...)
```

### Why Not Strategy A (Sub-package)?

Strategy A (`internal/tools/integrations/ado/format/`) would create a utility sub-package with a one-way dependency (ado → format). This is mechanically valid but:
- Risks becoming a "utility dumping ground" as new formatting needs arise
- Does not enforce the Hexagonal direction — the formatter sits next to the adapter, not at the consumer boundary
- Adds a new public package surface (`format.FormatPipelineRunsList`) that callers outside `ado` could import, creating unintended coupling

Strategy B is idiomatic Go ("accept interfaces, return structs") and enforces the dependency rule: Infrastructure → Domain, Presentation → Infrastructure.

## Consequences

### Positive
- **Hexagonal compliance**: ADO client is a pure infrastructure adapter returning domain structs
- **Testability**: Formatting can be tested independently from HTTP transport
- **Replaceability**: A different consumer (CLI vs. TUI vs. API) can provide a different formatter without touching the ADO client

### Negative
- **Call site changes**: Tool registration closures that currently call `Ado*` methods must be updated to receive raw structs and apply formatting
- **More files**: One additional file (`pipeline_format.go`) in the `ado` package

### Neutral
- `formatBranchRef` is borderline — it's partly domain logic (branch reference resolution) and partly presentation (the refs/heads/ convention). For this refactor, it stays in the formatting layer since its primary consumer is the run-pipeline formatter.

## Implementation Notes

1. This is a **structural refactor only** — no behavioral changes, no new features
2. All existing tests must continue to pass unmodified
3. The decomposition into `pipeline_crud.go`, `pipeline_runs.go`, `pipeline_logs.go` happens **after** formatter extraction
4. `verify_architecture` must show zero new edges after both steps
