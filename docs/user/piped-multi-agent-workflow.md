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
- **Function Form (`a`, `c`, etc.)**: Maintains persistent memory within the role for iterative refinement.

## Common Workflow Loops

### The Implementation Loop
1. **Plan**: `a -new "How to improve X?"`
2. **Translate**: `a "Tell Coder what to do."`
3. **Execute**: `a -l -r | c -new`
4. **Verify**: `c -l -r | a`

### The Quality Gate Loop
1. **Verify**: `t -new "Review last commit."`
2. **Mediate**: `t -l -r | a "Do you agree? --- "` (Architect acts as the judge)
3. **Fix**: `a -l -r | c -new`

### The Peer Review Loop
1. **Audit**: `r -new "Review last 2 commits."`
2. **Validate**: `r -l -r | a "Do you agree? --- "`
3. **Refine**: `a -l -r | c -new`

## Benefits
- **Separation of Concerns**: Each agent stays within its domain logic.
- **Reduced Context Noise**: Only relevant outputs are piped to the next agent.
- **Scalable Refinement**: Allows for multiple iterations of feedback without manual copying.

## Real-World Example

The following sequence illustrates a typical development cycle for `tell-me-go`:

```bash
# 1. Start planning with the Architect
a -new "How to improve this code base?"
a "Tell Coder what to do."

# 2. Pipe the plan to a new Coder session
a -l -r | c -new

# 3. Architect reviews Coder's implementation
c -l -r | a

# 4. Iterative refinement between Architect and Coder
a -l -r | c
c -l -r | a

# 5. Quality Assurance with the Tester
t -new "Review last commit."
t -l -r | a "Do you agree? --- "

# 6. Architect instructs Coder on Tester's feedback
a "Tell Coder what to do."
a -l -r | c -new
c -l -r | a

# 7. Final verification by Tester
t "Review last commit."

# 8. Peer Review for the last 2 commits
r -new "Review last 2 commits."
r -l -r | a "Do you agree? --- "

# 9. Final Architect-Coder-Reviewer cycle
a "Tell Coder what to do."
a -l -r | c -new
c -l -r | a
r "Review last commit."
```
