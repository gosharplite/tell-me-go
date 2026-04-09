### Description
The recent refactoring to centralize Go toolchain logic (resolving Issue #4) correctly implemented the "Return Structs" pattern but fundamentally broke the **"Accept Interfaces"** principle and introduced brittle error handling. This has led to tightly coupled domain services and fragile integration-style tests.

### Technical Debt Identified
1. **[Dependency Injection Violation]** The consumer components (`devManager`, `releaseManager` in `internal/tools/developer/`, and `healthManager` in `internal/tools/analysis/`) were updated to accept the concrete struct `*toolchain.GoRunner` via their constructors and struct definitions. In idiomatic Go, consumers should define and accept a local, narrowly-scoped interface (Interface Segregation Principle).
2. **[Brittle Error Handling]** `GoRunner.RunLinter` currently returns a raw string error (`"no supported linter found..."`). Consumers then parse this using `strings.Contains(err.Error(), "...")` to determine control flow (e.g., whether to skip linting or report an execution error). This string-matching is a known anti-pattern.

### Acceptance Criteria
- [ ] **Fix DI:** Update the constructors and struct definitions in `dev.go`, `release.go`, and `health.go` to accept locally defined interfaces (e.g., `type goRunner interface { ... }`) instead of the concrete `*toolchain.GoRunner`.
- [ ] **Sentinel Errors:** Define a sentinel error `var ErrNoSupportedLinter = errors.New("no supported linter found (golangci-lint or staticcheck)")` in the `toolchain` package and return it from `RunLinter`.
- [ ] **Idiomatic Error Checking:** Update the consumers (`health.go`, `dev.go`) to use `errors.Is(err, toolchain.ErrNoSupportedLinter)` and `errors.As(err, &exitErr)` (where `exitErr` is `*exec.ExitError`) instead of `strings.Contains`.
- [ ] **Clean Tests:** Refactor the tests (e.g., `dev_test.go`, `release_test.go`) to cleanly mock the restored interfaces without passing `nil` to constructors or performing awkward struct casting.
