# Piped Multi-Agent Workflow

This document describes the advanced human-in-the-loop (HITL) orchestration pattern used to develop `tell-me-go` using specialized role aliases.

## Core Concepts

### 1. Specialized Roles
The system utilizes distinct aliases and functions defined in `.bashrc` to isolate context:
- **`a` (Architect)**: High-level planning and technical specification.
- **`c` (Coder)**: Implementation and execution.
- **`t` (Tester)**: Verification and validation.
- **`r` (Reviewer)**: Quality assurance and commit auditing.

### 2. State Transfer via Pipes
Context is transferred between agents using the `-l -r` (Last-Raw) flags. This pipes the raw text output of one agent directly as the input prompt for another.
- **Syntax**: `agent1 -l -r | agent2`

### 3. Session Management
- **`*-new`**: Resets the specific agent's database/context for a fresh task.
- **Function Form (`a`, `c`, etc.)**: Maintains persistent memory within the role for iterative refinement.

## Common Workflow Loops

### The Implementation Loop
1. **Plan**: `a-new "How to improve X?"`
2. **Translate**: `a "Tell Coder what to do."`
3. **Execute**: `a -l -r | c-new`
4. **Verify**: `c -l -r | a`

### The Quality Gate Loop
1. **Verify**: `t-new "Review last commit."`
2. **Mediate**: `t -l -r | a "Do you agree? --- "` (Architect acts as the judge)
3. **Fix**: `a -l -r | c-new`

### The Peer Review Loop
1. **Audit**: `r-new "Review last 2 commits."`
2. **Validate**: `r -l -r | a "Do you agree? --- "`
3. **Refine**: `a -l -r | c-new`

## Benefits
- **Separation of Concerns**: Each agent stays within its domain logic.
- **Reduced Context Noise**: Only relevant outputs are piped to the next agent.
- **Scalable Refinement**: Allows for multiple iterations of feedback without manual copying.
