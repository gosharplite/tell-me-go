# dead_code_graph: Operator Reference & Interface Compliance Guards

> [!IMPORTANT]
> If you are about to unexport, delete, or rename an exported symbol based on a `dead_code_graph` finding: read §4 first, even if the report has no `[WARNING:]`. The analyzer is one input to the decision, not the decision.

## 1. Overview

`dead_code_graph` is the project's static-analysis tool for surfacing exported symbols with no observable inbound references inside the module. Its registered description is short by design: *"Identify exported symbols with zero inbound references within the module. This is a heavy operation that requires a go.mod file and scans the entire module to find technical debt."* (See `internal/tools/analysis/registration.go`.) Operators run it during refactoring rounds to find candidates for unexporting, deleting, or renaming.

A pure symbol-resolution scan cannot see every Go usage pattern. Structural typing, interface-satisfaction matching, anonymous interface assertions, and constructors that return types only consumed via inferred receivers all produce *call sites the analyzer cannot resolve to a name*. Over five hardening rounds, the analyzer learned to **auto-resolve** four such classes silently and to **emit a `[WARNING:]` hedge** for two more where automatic resolution is not justified. This document is the operator's authoritative reference for both halves of that split.

The governing principle is simple: *the analyzer auto-resolves what it can, hedges what it cannot, and the operator owns the final call.* First-time readers should read §2 to learn how to read a report. Operators triaging a specific finding should jump to §3 (the class catalog) and then §4 (the manual pre-flight protocol). Anyone considering an extension to the analyzer itself should consult §7.

This document's filename — `interface_compliance_guards.md` — predates its expanded scope and is retained for inbound-link stability.

## 2. How to Read a dead_code_graph Report

A `dead_code_graph` finding has the form:

```
<symbol>  pkg=<package-path>  type=<Function|Method|Type>  severity=<DEAD|PRIVATE>  reason=<text>[ [WARNING: …]]…
```

The two payload fields the operator must understand are **severity** and any **`[WARNING:]`** suffixes attached to the reason text.

### 2.1 Severity legend: [DEAD], [PRIVATE]

The analyzer assigns one of two severities (see `evaluateOrphan` in `internal/tools/analysis/dead_code.go`):

- **`[DEAD]`** — *No references found within the module (including interfaces/tests).* Total inbound usage count is zero. The symbol is either genuinely unused or is reached only through a pattern the analyzer cannot see.
- **`[PRIVATE]`** — *Exported symbol is only used within its own package.* The symbol has inbound references, but every one is in the declaring package; nothing outside imports its name. (When complexity ≥ 10, the reason is rephrased as a "High Priority Refactoring Candidate" — same severity, same meaning.)

Neither severity is a verdict. They are observations about what the analyzer's symbol-resolution scan saw. Whether the observation maps to genuine technical debt depends on whether the symbol participates in a pattern from §3.

### 2.2 Hedge legend: [WARNING: …]

The analyzer can append one of two `[WARNING:]` strings to a finding's reason. Both are operator-alerts and **do not** change the classification.

The exact strings, as emitted by `evaluateOrphan`:

- **Text-search hedge.** Appended whenever the symbol's bare name appears as a byte-substring in any file outside its declaring package:

  > `[WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]`

- **Anonymous-interface-assertion hedge.** Appended only on **method** orphans whose name appears as a method-shaped entry in any `*ast.TypeAssertExpr → *ast.InterfaceType` literal in module-internal packages:

  > `[WARNING: method name appears in anonymous-interface assertion site(s); verify with: grep -rn "<MethodName>" --include='*.go' .]`

Both warnings can fire on the same orphan; if so, both appear concatenated. The presence of a warning means the analyzer noticed a signal it cannot interpret on the operator's behalf — see §4.

### 2.3 The three-question triage

When a finding lands on your desk, walk these three questions in order:

1. **Does the entry have a `[WARNING:]`?**
   §4 manual pre-flight protocol is **mandatory**. The analyzer is telling you it cannot resolve the call site itself; you must.

2. **Does the symbol's name read like a structural-dispatch hook** — names matching the shape `Set...`, `Wrap...`, `On...`, or any other optional-capability method that a caller might invoke through an anonymous interface literal or a reflective bridge?
   §4 is **recommended**. These names are the most common shape for patterns the analyzer's automatic protections do not cover (notably Class D, but also reflection and DI). Spend the grep before you spend the refactor.

3. **Otherwise:** §4 is **optional but encouraged** before any unexport-or-delete. ADR `2026-04-blackbox-integration-test-tree` §5 makes a `grep` of the `tests/` tree mandatory for that specific concern; the §4 protocol generalizes the same instinct. The analyzer's automatic protections (§3.1) are conservative — known limitations exist (type aliases not propagated by Class B-prime; Class C only matches the literal filename `export_test.go`) — and the cost of a manual grep is much smaller than the cost of withdrawing a committed refactor.

The grep protocol is the operator's safety net, not a per-warning ritual.

## 3. False-Positive Class Catalog

The analyzer protects against five *named* false-positive classes plus one *generic safety hedge*. The split between **automatic** (§3.1, no operator action) and **operator-verifies** (§3.2 plus the meta-hedge in §3.3, warning emitted) is the conceptual spine of this document. Each card below uses the same five-bullet shape so adjacent classes can be compared at a glance.

### 3.1 Automatic classes (no operator action)

The classes in this section are resolved silently inside the analyzer's pipeline. The operator sees no warning and takes no action; the symbol simply does not appear in the orphan report. Cards exist so that *if* one of these classes regresses (e.g., a future refactor breaks the relevant pass), the operator has the vocabulary to recognize the regression.

#### 3.1.1 Class A — Stdlib interface satisfaction

- **Symptom:** A method whose name is one of a small set of stdlib interface methods (e.g., `Read`, `Write`, `Close`, `ServeHTTP`) is flagged `[DEAD]` or `[PRIVATE]`. Should never appear in a healthy report.
- **Cause:** Methods invoked through stdlib interfaces such as `io.Reader`, `io.Writer`, `io.Closer`, or `http.Handler` are dispatched structurally; the method's name is never written at the call site, so symbol resolution sees nothing.
- **Status:** Auto-protected (no operator action).
- **Implementation:** `isWellKnownContract` in `internal/tools/analysis/dead_code.go` consults the `wellKnownContractMethods` table (the authoritative list of recognized contracts; do not enumerate it elsewhere) and returns true on a name + structural-signature match (per `signatureMatches`, with `interface{}` ↔ `any` and `[]uint8` ↔ `[]byte` normalization in `canonicalTypeString`). Pinned by `TestIsWellKnownContract_Table`, `TestIsWellKnownContract_RealStdlib`, `TestIsWellKnownContract_Negative_Var`, and `TestCanonicalTypeString_Aliases` in `dead_code_contracts_test.go`.
- **Operator action:** None. If you do see one of these methods flagged anyway, the protection table is missing your case (e.g., `sort.Interface` and `driver.Valuer` are deliberate omissions because their names are too common). File an analyzer issue with the orphan's name and full signature; do not silence it locally.

#### 3.1.2 Class B — External _test package consumers

- **Symptom:** An exported symbol consumed only by an external `package foo_test` file is flagged `[DEAD]` or `[PRIVATE]`. Should never appear in a healthy report.
- **Cause:** Symbols referenced from `package foo_test` files live in a synthesized test package distinct from the production package; if the indexer does not load those synthesized packages, their references are invisible to the analyzer.
- **Status:** Auto-protected (no operator action).
- **Implementation:** `loadPackages` in `internal/tools/analysis/index.go` sets `Tests: true` on its `packages.Config`, causing `go/packages` to load the synthesized `foo`, `foo_test`, and `foo.test` variants. Pinned at the indexer layer by `TestIndexerLoadsTestPackages` and at the analyzer layer by `TestDeadCodeAnalyzer_ExternalTestConsumesMethod` in `dead_code_test_consumer_test.go`. The `Tests: true` doc-comment in `index.go` names the contract and points to the pinning test by name.
- **Operator action:** None. The related operator rule — *static-analysis tools must be scoped to the whole module (`./...`)* — is binding and lives in ADR `2026-04-blackbox-integration-test-tree` §4; do not invoke `dead_code_graph` on a subdirectory subset.

#### 3.1.3 Class B-prime — Constructor return-type propagation

- **Symptom:** A type consumed only via inferred receiver (e.g., `mc := foo.NewMockClock(); mc.Advance(...)`) is flagged `[PRIVATE]`. Should never appear for the standard pattern; may appear for type-alias edge cases (see Operator action).
- **Cause:** When a caller writes `mc := foo.NewMockClock()`, the type identifier `MockClock` never textually appears outside the declaring package; only the constructor name does. Symbol resolution counts the constructor as used but cannot infer that the returned type is therefore also used.
- **Status:** Auto-protected (no operator action).
- **Implementation:** `propagateConstructorUsagesToReturnTypes` in `internal/tools/analysis/dead_code.go` runs as a pipeline pass after `propagateInterfaceUsages`; for each used function/method, it flows the external-use count to each named non-interface return type (with one level of pointer unwrap). Helper: `extractNamedReturnTypes`. Pinned by the seven-test suite in `dead_code_constructor_propagation_test.go`, headlined by `TestConstructorPropagation_HeadlineCase`. Two design pins worth knowing about: the type is protected but its methods are evaluated independently (`TestConstructorPropagation_MethodsNotTransitivelyProtected`), and type aliases are not propagated through (`TestConstructorPropagation_TypeAliasNotPropagated`).
- **Operator action:** None for the standard pattern. **Limitation:** if the type is reached only through a type alias (`type Exported = unexported`), Class B-prime cannot see it. The most common alias case — aliases declared in `export_test.go` — is covered separately by Class C; for any other alias case, run §4 before acting.

#### 3.1.4 Class C — export_test.go alias declarations

- **Symptom:** An alias or thin wrapper declared in a file named exactly `export_test.go` is flagged `[DEAD]` or `[PRIVATE]`. Should never appear in a healthy report.
- **Cause:** `export_test.go` is a Go convention for files that exist solely to bridge a production package with its external `_test` package — typically by re-exporting internal types as aliases (`type Exported = unexported`) or wrapping unexported functions with thin exported shims. Such declarations are test-API surface, not production code; flagging them is a category error.
- **Status:** Auto-protected (no operator action).
- **Implementation:** `isExportTestFile` in `internal/tools/analysis/dead_code.go` filters out declarations whose source file's basename is exactly `export_test.go`, applied at the harvest chokepoint `harvestObjectSymbols` and defensively at `harvestNamedMethods` and `harvestInterfaceMethods`. Pinned by `TestExportTestAlias_BareAliasNotFlagged`, `TestExportTestAlias_FunctionInExportTestSuppressed`, `TestExportTestAlias_OrdinaryTestGoStillFlagged`, and `TestExportTestAlias_ProductionDeclarationsUnaffected` in `dead_code_export_test_alias_test.go`.
- **Operator action:** None for the literal filename `export_test.go`. **Narrow scope by design:** other `_test.go` files (e.g., `helpers_test.go`) are still subject to analysis. An unused exported test helper *is* genuine technical debt and should be acted on.

### 3.2 Operator-verifies classes (warning emitted)

The class in this section is one the analyzer cannot resolve on its own. It emits a `[WARNING:]` and asks the operator to verify manually via §4.

#### 3.2.1 Class D — Anonymous-interface assertion dispatch

- **Symptom:** A method orphan carries the warning `[WARNING: method name appears in anonymous-interface assertion site(s); verify with: grep -rn "<MethodName>" --include='*.go' .]`.
- **Cause:** Go supports `x.(interface{ M() })` for optional-capability dispatch. The asserted-into interface literal has no declaration to resolve against, so symbol resolution cannot detect the call.
- **Status:** Operator-verifies (warning emitted).
- **Implementation:** `hasAnonymousInterfaceAssertionMatch` in `internal/tools/analysis/dead_code_anon_interface.go` consults a lazily-built set of method names harvested from anonymous interface literals in `*ast.TypeAssertExpr` nodes across module-internal packages. By design the match is **name-only** (signature collisions cause an extra warning, never silent over-protection), restricted to **method-shaped** entries (embedded interfaces are skipped — they are already covered by `propagateInterfaceUsages`), and restricted to **module-internal packages**. Pinned by `TestAnonymousInterfaceAssertionWarning_FiresOnMatchingMethodName`, `TestAnonymousInterfaceAssertionWarning_DoesNotFireOnUnrelatedMethodName`, `TestAnonymousInterfaceAssertionWarning_DoesNotFireOnFreeFunction`, `TestAnonymousInterfaceAssertionWarning_IgnoresEmbeddedInterfaces`, and the live-codebase pin `TestAnonymousInterfaceAssertionWarning_LiveCodebaseHeadlinePin` in `dead_code_anon_interface_test.go`.
- **Operator action:** Run §4 manual pre-flight protocol. The exact `grep` command is embedded in the warning string itself.

### 3.3 Generic safety hedge: text-search cross-package match

This is **not** a named false-positive class. It is a last-resort substring scan the analyzer emits whenever it has any reason to suspect — by the coarsest possible measure — that the symbol's name appears outside its declaring package. It exists so the analyzer never silently delivers a `[DEAD]`/`[PRIVATE]` verdict on a symbol whose name is mentioned anywhere else in the module.

- **Symptom:** Orphan with `[WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]`
- **Trigger:** The analyzer reads every `.go` file in every package other than the symbol's declaring package and does a literal byte-substring search for the symbol's bare name; the warning fires on any hit, including hits inside comments, string literals, and unrelated identifiers that share the spelling.
- **Status:** Operator-verifies (warning emitted). Not tied to any specific Go pattern; this is a last-resort hedge.
- **Implementation:** `hasTextMatchOutsidePackage` in `internal/tools/analysis/dead_code.go`, called from `evaluateOrphan` immediately before the Class D hedge. Both warnings can fire on the same orphan.
- **Operator action:** Run §4 manual pre-flight protocol.

## 4. Manual Pre-Flight Protocol

This is the canonical workflow for verifying any `dead_code_graph` finding the operator does not trust at face value. It is the **authoritative answer** for both warning-bearing entries (Class D and the §3.3 meta-hedge) and any other entry where the §2.3 triage prompts deeper investigation.

### 4.1 When to run it

Run the protocol whenever any of the following holds:

- The finding carries a `[WARNING:]` of any kind. **(Mandatory.)**
- The symbol is a method whose name reads like a structural-dispatch hook (`Set...`, `Wrap...`, `On...`, optional-capability surfaces). **(Recommended.)**
- You are about to unexport, delete, or rename an exported symbol on the strength of any `[DEAD]`/`[PRIVATE]` finding. **(Optional but encouraged.)** ADR `2026-04-blackbox-integration-test-tree` §5 makes the corresponding `tests/`-tree grep mandatory for that specific concern; the protocol generalizes to the whole module.

You also run the protocol when the analyzer's automatic protections have a known limitation that might apply: the suspected symbol is reached only through a type alias (Class B-prime gap) or through a file with a `_test.go` basename other than the literal `export_test.go` (Class C narrow scope).

### 4.2 The grep workflow

The exact command — the one Class D's warning embeds inline — is:

```bash
grep -rn "<SymbolName>" --include='*.go' .
```

Read the output by classifying each hit into one of four buckets:

1. **Declaring package.** Discard. The symbol of course appears inside its own package.
2. **Confirmed call site.** A line of the form `x.<SymbolName>(`, `pkg.<SymbolName>(`, `<SymbolName>(...)`, or a value-context use such as `var f = pkg.<SymbolName>`. The symbol is live.
3. **Type-assertion or interface-literal entry.** A line containing `interface{ ...<SymbolName>(...) }` (the Class D pattern), or `_ = (*pkg.<SymbolName>)(nil)`, or any other shape that proves the name is being referenced *as a Go identifier* by the compiler. The symbol is live.
4. **Comment, string, or unrelated identifier.** A `// <SymbolName>` reference, a `"<SymbolName>"` string-literal mention, or a same-spelling identifier in an unrelated package (Go's package-scoped namespacing means `Close` in package `foo` and `Close` in package `bar` are distinct symbols even with identical names). These do not count.

For symbols where the upstream concern is `tests/`-tree consumption specifically — the original motivating case for ADR `2026-04` §5 — narrow the scope: `grep -rn "<SymbolName>" tests/`. Same classification rules.

### 4.3 What constitutes a confirmed call site

A finding may be treated as a false positive — and the symbol therefore as live — only if the grep output yields **at least one hit in bucket 2 (confirmed call site) or bucket 3 (type-assertion or interface-literal entry)** outside the declaring package. Hits in bucket 4 alone are not sufficient; comments and strings do not produce a compile-time edge.

If no bucket-2 or bucket-3 hit is found, the orphan is genuine and proceed to §5 to decide whether to act on it. If a confirmed call site is found, do not act on the orphan; if appropriate, follow up by recording the protection per §5.3.

## 5. When to Override the Analyzer

The analyzer's verdicts are observations, not decisions. An override is the operator's act of treating a `[DEAD]`/`[PRIVATE]` finding as a false positive and choosing not to delete, unexport, or rename the symbol. Overrides are legitimate but must be justified, narrow, and recorded.

### 5.1 Justified overrides

An override is justified when one of the following applies:

- **Verified by §4.** The grep protocol produced at least one confirmed call site outside the declaring package. The analyzer's symbol-resolution scan missed a Go pattern it cannot see.
- **Stdlib interface contract not in the Class A table.** The symbol participates in a stdlib structural contract that the `wellKnownContractMethods` table omits — most commonly `sort.Interface` (`Len`/`Less`/`Swap` triple) or `driver.Valuer`. The right long-term fix is an analyzer issue to extend the table; the short-term fix is a documented override.
- **Dynamic dispatch the analyzer cannot trace.** The symbol is the target of a Dependency Injection framework, reflection, build-tagged code paths, generated code, or any other mechanism that invokes the symbol without writing its name in a tractable AST node.

In every case the operator must add an inline comment explaining *which class* of false positive the override silences and, for the stdlib-table-extension case, the analyzer issue number. An override without that comment is indistinguishable from forgotten technical debt.

### 5.2 Unjustified overrides (anti-patterns)

If the code is genuinely unused in the production execution path and is being kept "just in case," using the blank identifier or any other technique to trick the analyzer is **sweeping technical debt under the rug**.

* *Rule:* Do not do this. Delete the code (YAGNI — You Aren't Gonna Need It). Source control (Git) remembers it if you ever need it back.

The corresponding signs: a `// might be useful later` comment paired with a blank-identifier assertion; a `// re-enable this when we add the X feature` paired with a dummy reference; an external `_test` reference whose only consumer is the dummy itself. Each is a maintenance burden the analyzer was specifically trying to surface.

### 5.3 If override is justified: how to record it

Record the override using the most architecturally faithful technique from §6 that fits the case. Preference order:

1. **§6.1 blank-identifier interface compliance assertion** if the symbol is a struct that implements an interface. This is the strongest form: the override is a *compile-time check* that doubles as analyzer protection.
2. **§6.3 hexagonal fix (unexport the struct)** if the symbol is a domain service struct exported only because of historical convention; this resolves the false positive *and* the architectural smell in one move.
3. **§6.2 external `_test` dummy reference** only as a last resort, and only for residual cases the propagation passes cannot reach.

Whichever technique is used, the inline comment must name the false-positive class (`Class A omission`, `Class D`, `reflective dispatch`, etc.) and, where relevant, link to the analyzer issue tracking the gap. Reviewers should reject any blank-identifier assertion or dummy reference whose comment does not justify itself in those terms.

## 6. Manual Remediation Techniques (Reference)

This section is the reference for the techniques used in §5.3. The wording for §6.1 and §6.3 is preserved from the previous edition of this document; both techniques are still correct and used today. §6.2 is downgraded to an escape hatch — see its preamble.

### 6.1 Blank-identifier interface compliance assertion

If the component is a struct that implements an interface, the most architecturally sound way to silence a false positive is an interface compliance check. This proves at compile-time that the struct satisfies the port:

```go
package services

import "github.com/gosharplite/tell-me-go/internal/domain/ports"

// Ensure TaskService strictly implements the ports.TaskStore interface
// (This also prevents false positives in dead_code_graph AST analysis)
var _ ports.TaskStore = (*taskService)(nil)
```

This pattern has two payoffs that are independent of the analyzer: (1) it guarantees that any future change to either the struct or the interface that breaks the contract fails the build, and (2) it documents the intended port-to-adapter binding inline in the source. The analyzer-protection effect is a side benefit, not the primary motivation.

### 6.2 External _test dummy reference (legacy; rarely needed post-B-prime)

> **Status: Reference only.** As of commit `61a04423` (Class B-prime), this manual technique is no longer required for the standard pattern of a struct returned from an exported constructor and consumed via inferred receiver in a `_test` file. The analyzer auto-protects that case. This section is retained for the residual cases where the propagation pass cannot reach the type — typically reflective bridges, build-tagged code paths, or types reached only through interface-typed indirection. **If you find yourself reaching for this technique on a non-residual case, file an analyzer issue first.**

Sometimes an analyzer flags an exported type (e.g., `TaskService`) because it is never explicitly written as a type outside of its own package, even though it's dynamically upcast to an interface via DI.
To silence the graph without changing domain code, a dummy reference can be placed in a test file *outside* the package (using the `_test` suffix):

```go
// internal/domain/services/task_service_external_test.go
package services_test // Note the _test suffix!

import "github.com/gosharplite/tell-me-go/internal/domain/services"

// Prove the exported type can cross package boundaries
var _ = (*services.TaskService)(nil)
```

The accompanying comment must satisfy §5.3: name the false-positive class the dummy is silencing, and explain why the standard auto-protection cannot reach it. A dummy without that justification is a maintenance hazard.

### 6.3 The hexagonal fix (unexport the struct)

If `dead_code_graph` flags an exported domain service struct (e.g., `TaskService`) because it is only used via its interface (`ports.TaskStore`), the real architectural issue is often that **the concrete struct should not be exported in the first place**. This is frequently the architecturally cleanest answer when the analyzer flags an exported domain service.

In Clean Architecture, your constructor should be exported, but the concrete struct it returns should remain hidden:

```go
// internal/domain/services/task_service.go

// Unexported struct!
type taskService struct {
    // ...
}

// Constructor returns the unexported type (or the interface directly)
func NewTaskService(store ports.ListStore[ports.Task]) *taskService {
    return &taskService{ ... }
}
```
By unexporting the struct (`taskService`), static analysis tools will correctly ignore it, as they are designed to assume unexported symbols are private to their package.

## 7. Maintainer Notes

This section is for engineers extending or modifying the analyzer itself. Operators triaging a finding should not need it.

### 7.1 Architecture of the protection passes

The pipeline order in `runAnalysisPipeline` (`internal/tools/analysis/dead_code.go`) is: `harvestExportedSymbols` → `analyzeUsages` → `propagateInterfaceUsages` → `propagateConstructorUsagesToReturnTypes` → orphan emission via `evaluateOrphan` (with the §3.2 and §3.3 hedges applied at emission time). Both propagation passes operate on a snapshot of pre-pass usage state to avoid cascade effects; see source for details. Per-pass entry points: `propagateInterfaceUsages` and `propagateConstructorUsagesToReturnTypes` for the propagation phases; `isWellKnownContract` for the Class A check; `isExportTestFile` for the Class C harvest filter; `hasAnonymousInterfaceAssertionMatch` (in `dead_code_anon_interface.go`) and `hasTextMatchOutsidePackage` for the two `[WARNING:]` hedges.

### 7.2 Known limitations

- **Class A omits `sort.Interface` and `driver.Valuer`.** `Len`/`Less`/`Swap` are too common as standalone names to protect in isolation; `driver.Valuer.Value() (driver.Value, error)` returns `any` and is therefore signature-ambiguous with many unrelated user methods. Any future extension must either match the `sort.Interface` triple as a whole or accept the false-negative risk.
- **Class B-prime does not propagate through type aliases.** `(*types.Named).Obj()` resolves to the underlying type's `TypeName`, not the alias's, so the alias declaration is not reached. The most common case (aliases in `export_test.go`) is covered by Class C. Other alias cases require an §5 override.
- **Class D matches names only, not signatures.** Method-name collisions with stdlib interfaces (`Close`, `Read`, `String`) cause extra `[WARNING:]` annotations, never silent over-protection. The cost of a stricter signature match was not justified at the current false-positive count of one.
- **Text-search hedge is a literal byte-substring scan.** It will fire on the symbol name appearing inside comments, string literals, and unrelated identifiers in other packages. The §4.3 bucket-classification step is the prescribed filter.

### 7.3 Deferred work: Task D-Future

A full structural-dispatch propagation pass — the analyzer-side analogue of Class D's manual grep — was designed and rejected during Class D pre-flight on cost/benefit grounds. Cost was estimated at roughly 200 LOC plus permanent maintenance of `go/types` identity logic; benefit at the time of decision was one current false positive (`(SecurityManager).SetInteractor`). The full design is preserved in GitHub issue #69 as Task D-Future. The trigger to revisit is documented there.

## 8. References

**Implementing commits (8-char short hashes):**

| Hash | Class | Subject |
|---|---|---|
| `7f49d1ba` | A | Stdlib interface satisfaction protection |
| `d2a47f39` | B | External `_test` package consumer contract pin |
| `61a04423` | B-prime | Constructor → return-type usage propagation |
| `5ee7fcb5` | C | `export_test.go` declaration suppression |
| `7f93e184` | D | Anonymous-interface assertion warning hedge |

**Source files:**

- `internal/tools/analysis/dead_code.go` — analyzer entry points, harvest/propagation/emission, Class A check, Class C filter, text-search hedge.
- `internal/tools/analysis/dead_code_anon_interface.go` — Class D warning implementation.
- `internal/tools/analysis/index.go` — `loadPackages` with `Tests: true` (Class B contract); see the inline doc-comment.
- `internal/tools/analysis/registration.go` — `dead_code_graph` tool's user-facing declaration.

**Pinning tests (one canonical entry per class):**

- Class A — `TestIsWellKnownContract_Table` in `internal/tools/analysis/dead_code_contracts_test.go`
- Class B — `TestIndexerLoadsTestPackages` in `internal/tools/analysis/dead_code_test_consumer_test.go`
- Class B-prime — `TestConstructorPropagation_HeadlineCase` in `internal/tools/analysis/dead_code_constructor_propagation_test.go`
- Class C — `TestExportTestAlias_BareAliasNotFlagged` in `internal/tools/analysis/dead_code_export_test_alias_test.go`
- Class D — `TestAnonymousInterfaceAssertionWarning_FiresOnMatchingMethodName` in `internal/tools/analysis/dead_code_anon_interface_test.go`

**Related architectural decision record:**

- `docs/adr/2026-04-blackbox-integration-test-tree.md` §4 (module-wide scope mandate for static analysis) and §5 (mandatory `grep tests/` pre-flight before unexporting).

**GitHub issue:**

- `#69` — canonical record of the false-positive series and home of Task D-Future's design.
