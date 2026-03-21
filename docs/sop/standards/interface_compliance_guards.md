# Architectural Guidance: Interface Compliance Guards and Dead Code Analysis

## Overview

In Go development, particularly when adhering to Clean Architecture and Hexagonal Design, it is common to use the **blank identifier (`_`)** for type assignments. This technique serves two primary purposes:
1. Enforcing strict compile-time checks for interface implementation.
2. Resolving "false positives" in static analysis tools (like `dead_code_graph` or AST parsers) caused by Dependency Injection (DI) and structural typing.

This document outlines the mechanism, architectural guidance, and specific applications of this pattern within the repository.

---

## The Technique: The Blank Identifier Assignment

To prove interface compliance or silence a dead code analyzer without executing code, we drop a global variable assignment to the blank identifier (`_`). 

### 1. The "Interface Compliance" Guard (Highly Recommended)
If the component is a struct that implements an interface, the most architecturally sound way to use this trick is an interface compliance check. This proves at compile-time that the struct satisfies the port:

```go
package services

import "github.com/gosharplite/tell-me-go/internal/domain/ports"

// Ensure TaskService strictly implements the ports.TaskStore interface
// (This also prevents false positives in dead_code_graph AST analysis)
var _ ports.TaskStore = (*taskService)(nil)
```

### 2. The Test File Trick (Cross-Package Dummy Reference)
Sometimes an analyzer flags an exported type (e.g., `TaskService`) because it is never explicitly written as a type outside of its own package, even though it's dynamically upcast to an interface via DI.
To silence the graph without changing domain code, a dummy reference can be placed in a test file *outside* the package (using the `_test` suffix):

```go
// internal/domain/services/task_service_external_test.go
package services_test // Note the _test suffix!

import "github.com/gosharplite/tell-me-go/internal/domain/services"

// Prove the exported type can cross package boundaries
var _ = (*services.TaskService)(nil)
```

---

## Architectural Pushback: When to Use This Pattern

### ✅ Valid Use Case 1: Compile-Time Interface Safety
Using the guard to verify that unexported structs (`*taskService`) or mock structs (`*mockGateway`) correctly implement their required interfaces.

### ✅ Valid Use Case 2: False Positives from DI / Reflection
If a component is *actually* used in production but injected dynamically via a Dependency Injection framework (like Google Wire, Uber Fx) or reflection that an AST-based analyzer cannot trace. 
* *Rule:* You must add a comment explaining *why* it is silenced.

### ❌ Invalid Use Case: Hiding Actual Dead Code (Anti-Pattern)
If the code is genuinely unused in the production execution path and is being kept "just in case," using the blank identifier to trick the analyzer is **sweeping technical debt under the rug**.
* *Rule:* Do not do this. Delete the code (YAGNI - You Aren't Gonna Need It). Source control (Git) remembers it if you ever need it back.

---

## The Hexagonal Fix (Architecturally Superior)

If `dead_code_graph` flags an exported domain service struct (e.g., `TaskService`) because it is only used via its interface (`ports.TaskStore`), the real architectural issue is often that **the concrete struct should not be exported in the first place**.

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

---

## Current Usage in the Repository

An audit of the repository demonstrates that this pattern is used in a **100% architecturally sound** manner. It is not used maliciously to hide dead production code. Instead, it acts as a compile-time assertion:

**1. Protecting Mocks (in `_test.go` files):**
Proving to the compiler that locally defined mock structs satisfy a specific interface.
* `internal/domain/llmcoord/service_test.go`: `var _ llm.LLMGateway = (*mockGateway)(nil)`
* `internal/domain/monitoring/service_test.go`: `var _ pricing.CostTracker = (*mockCostTracker)(nil)`
* `internal/tools/developer/dev_test.go`: `var _ domain_security.ActionConfirmer = (*security.SecurityManager)(nil)`

**2. Protecting Production Services:**
Ensuring that unexported or DI-injected production services strictly implement their required domain port.
* `internal/domain/llmcoord/service.go`: `var _ orchestration.LLMCoordinator = (*service)(nil)`
* `internal/domain/monitoring/service.go`: `var _ orchestration.MonitoringTracker = (*service)(nil)`
* `internal/domain/services/task_service.go`: `var _ ports.TaskStore = (*TaskService)(nil)`

By adhering to these patterns, the system maintains strict compile-time safety and a pristine architectural dependency graph.
