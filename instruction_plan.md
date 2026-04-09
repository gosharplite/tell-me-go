## Implementation Plan & Instructions for Coder

Please follow this step-by-step plan to resolve the architectural and DI regressions introduced in PR #21 / Issue #4.

### Phase 1: Fix Error Handling in `toolchain` Package
**File:** `internal/service/toolchain/runner.go`
1. Define a package-level sentinel error:
   ```go
   var ErrNoSupportedLinter = errors.New("no supported linter found (golangci-lint or staticcheck)")
   ```
2. Update the `RunLinter` method to return this sentinel error instead of an inline `errors.New` string:
   ```go
   // Change: return "", "", errors.New(...)
   // To:
   return "", "", ErrNoSupportedLinter
   ```

### Phase 2: Fix Consumer Error Checking
**File:** `internal/tools/analysis/health.go`
1. Remove the brittle `strings.Contains` checks in the `runLint` method.
2. Use idiomatic `errors.Is` and `errors.As` (with `*exec.ExitError`) to handle control flow.
   ```go
   import "os/exec" // Ensure this is imported

   // Inside runLint(ctx context.Context) (string, string):
   outStr, tool, err := m.Runner.RunLinter(ctx)
   
   var exitErr *exec.ExitError
   if err != nil && !errors.As(err, &exitErr) {
       if errors.Is(err, toolchain.ErrNoSupportedLinter) {
           return "SKIP", "No linter found"
       }
       return "ERROR", err.Error()
   }
   ```

### Phase 3: Fix Dependency Injection (Accept Interfaces)
**File:** `internal/tools/developer/dev.go`
1. The file already defines a `goRunner` interface, but the constructor explicitly asks for the concrete struct. Fix the constructor signature:
   ```go
   // Change:
   func newDevManager(sm devSecurity, validator domain_security.CommandValidator, runner *toolchain.GoRunner, opts ...devOption) *devManager
   
   // To:
   func newDevManager(sm devSecurity, validator domain_security.CommandValidator, runner goRunner, opts ...devOption) *devManager
   ```

**File:** `internal/tools/developer/release.go`
1. Define a focused interface at the top of the file for the Go runner capabilities needed by the release process:
   ```go
   type releaseGoRunner interface {
       RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
       RunLinter(ctx context.Context) (string, string, error)
       CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
   }
   ```
2. Update the `releaseManager`, `linterChecker`, `buildChecker`, and `testRunner` structs to embed `releaseGoRunner` instead of `*toolchain.GoRunner`.

### Phase 4: Clean Up Tests
**File:** `internal/tools/developer/dev_test.go`
1. Now that `newDevManager` accepts the interface again, fix the awkward instantiation in tests:
   ```go
   // Change:
   m := newDevManager(sm, validator, nil)
   
   // To:
   runner := &mockGoRunner{}
   m := newDevManager(sm, validator, runner)
   ```

**File:** `internal/tools/developer/release_test.go`
1. Refactor the tests so they can cleanly pass a mock implementation of `releaseGoRunner` (like a `mockReleaseRunner` struct) instead of having to instantiate a real `toolchain.GoRunner` wrapping a `mockToolchainExecutor`.

### Phase 5: Verification
Run the following commands to ensure tests pass and the code is clean:
```bash
go test ./internal/tools/developer/...
go test ./internal/tools/analysis/...
go test ./internal/service/toolchain/...
golangci-lint run
```
