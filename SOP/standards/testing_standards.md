// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT


# Standard Operating Procedure (SOP): Testing Standards and Organization

### Objective
To ensure that the `tell-me-go` test suite remains modular, isolated, and utilizes idiomatic Go testing patterns for high reliability and maintainability.

---

### Prerequisites
- Go toolchain 1.24+.
- Familiarity with the standard `testing` package.
- (Optional) `testify` or similar assertion libraries if adopted later.

---

### Step-by-Step Instructions

#### 1. Locality and Naming
In Go, tests live alongside the source code to allow access to unexported members and ensure package-level integrity.
- **Unit Tests**: Place in the same directory as the logic being tested. File name must end in `_test.go` (e.g., `pkg/history/manager_test.go`).
- **Integration Tests**: Place in a top-level `test/` directory or within the package with a `//go:build integration` build tag.
- **End-to-End (E2E) Tests**: Place in `tests/e2e/`. E2E tests MUST build the application binary and execute it as a separate process to verify the CLI contract.
- **Function Naming**: Test functions must start with `Test` followed by a capitalized name (e.g., `func TestPruneHistory(t *testing.T)`).

#### 2. Environment Isolation
Every test must be isolated from the host system and other tests:
1.  **Temporary Workspace**: Always use `t.TempDir()` to create a workspace that is automatically cleaned up after the test.
2.  **Concurrency**: Use `t.Parallel()` where possible to speed up execution.
3.  **TELL_ME_HOME**: For E2E and integration tests, explicitly set the `TELL_ME_HOME` environment variable to a temporary directory to avoid side effects on the developer's local state.
4.  **No Side Effects**: Never modify global environment variables or local configuration files (`~/.config`) directly. Use dependency injection or environment mocking.

#### 3. Mocking Dependencies
- **Interfaces**: Define interfaces for external dependencies (API clients, File System, Git) to allow easy mocking in tests.
- **Offline E2E Mocking**: E2E tests MUST NOT hit live Gemini/Vertex APIs. Use `httptest.NewServer` and redirect the CLI using the `TELL_ME_MOCK_URL` environment variable.
- **Interactive Handshakes**: For tests involving user confirmation (y/N), use the `TELL_ME_MOCK_ANSWER` environment variable to simulate user input.
- **Complex API Mocks**: When testing components that interact with the Gemini API (like the `agent` package), the mock server **MUST** simulate multi-part responses, including:
    - `thought` and `thoughtSignature` fields.
    - `functionCall` and `functionResponse` roles.
- **Provider Compliance**: Mocks must enforce the schema requirements of the strictest provider (Vertex AI). For example, a mock should fail if a `thoughtSignature` is returned by the model but not preserved in subsequent history payloads sent by the system under test.

#### 4. Test Execution
- **Run All Tests**:
  ```bash
  go test ./...
  ```
- **Run with Race Detector**: (Required before merge)
  ```bash
  go test -race ./...
  ```
- **Check Coverage**:
  ```bash
  go test -coverprofile=coverage.out ./...
  go tool cover -html=coverage.out
  ```

---

### Code Templates

#### Unit Test Boilerplate:
```go
package history
- **Strict Provider Compliance**: When mocking external APIs (like Gemini), the mock must enforce the schema requirements of the **strictest** available provider (e.g., Vertex AI). Passing a mock is only valid if the mock validates mandatory fields (like `role`) that strict providers require.

import (
	"testing"
	"os"
	"path/filepath"
)

func TestSaveHistory(t *testing.T) {
	t.Parallel() // Enable parallel execution

	// 1. Setup isolated environment
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")

	// 2. Setup system under test
	manager := NewManager(historyFile)

	// 3. Execution
	err := manager.AddEntry("User", "Hello")
	if err != nil {
		t.Fatalf("Failed to add entry: %v", err)
	}

	// 4. Verification
	if _, err := os.Stat(historyFile); os.IsNotExist(err) {
		t.Errorf("History file was not created")
	}
}
```

---

### Verification & Testing
1.  **Linting**: Run `go vet ./...` to catch common testing mistakes (e.g., wrong signature).
2.  **Execution**: Ensure `go test ./...` returns **PASS**.
3.  **Pre-commit Hook**: Verification is enforced by the pre-commit requirements in `SOP/standards/git_workflow.md`.

---

### Best Practices
- **Table-Driven Tests**: Use anonymous structs and loops for testing multiple edge cases efficiently.
- **Clean Assertions**: Keep tests readable. If an assertion is complex, wrap it in a helper function.
- **Resilience**: Mock API errors (429, 500, timeouts) to verify the client's retry logic.
- **Safety Boundary Testing**: Tests for resource limits (turn-count, token-count) must verify that the system returns a graceful error (e.g., `ErrContextLimitExceeded`) rather than terminating the process.
- **Golden Files**: Use "golden files" for large expected outputs (like complex JSON history structures).
- **No Style in Assertions**: If comparing terminal output, strip ANSI escape codes before asserting.
