# ADR-012: Dynamic Tool Discovery via Capability Toolkits

## 1. Context and Problem Statement
As `tell-me-go` expands its integrations (File System, Git, Kubernetes, Azure DevOps, Jira, etc.), the number of registered `ToolDeclarations` has grown significantly. 
Currently, all available tools are injected into the LLM's context (system prompt / tool schema) on every request. 

**Architectural Blockers & Impacts:**
*   **Token Bloat (Cost & Limits):** Sending 50+ tool schemas on every turn consumes massive amounts of input tokens, driving up costs and eating into the context window.
*   **Attention Dilution:** Providing too many tools simultaneously degrades the LLM's ability to focus and select the correct tool, leading to hallucinations or incorrect tool usage.
*   **Static Binding:** The system cannot scale to hundreds of tools without exceeding hard API limits for tool schemas.

## 2. Decision
We will implement the **Capability Toolkit** pattern to enable lazy-loading of domain-specific tools by the LLM itself.

1.  **Core vs. Domain Tools:** 
    *   **Core Tools** (always active): File reading/writing, basic shell execution, and `load_toolkit`.
    *   **Domain Toolkits** (lazy-loaded): Grouped by domain (e.g., `git`, `k8s`, `ado`, `jira`).
2.  **The `load_toolkit` Tool:** The LLM will be provided with a permanent core tool: `load_toolkit(names []string)`. When the user asks a question requiring domain knowledge, the LLM will first call this tool.
3.  **Additive Session State:** Once a toolkit is loaded via `load_toolkit`, it is appended to the `SessionState` and remains active for the duration of the conversation history window.

## 3. Rationale
*   **Why Toolkits over individual tool retrieval?** Using embeddings or vector search to find individual tools (e.g., finding `k8s_apply` vs `k8s_get`) is overkill and error-prone. Grouping tools by domain (`kubernetes`) matches how humans think about workflows and requires fewer LLM roundtrips.
*   **Why Additive State?** *CRITICAL API CONSTRAINT:* Most LLM APIs (OpenAI, Gemini) will return a `400 Bad Request` if the conversation history contains a `FunctionCall` for a tool that is no longer present in the active tool schema. Therefore, tools cannot be easily unloaded while their history exists. They must be additively loaded and only cleared when the conversation history is summarized/truncated.

## 4. Consequences
### Positive
*   **[SCALABILITY]** The framework can now support an infinite number of tools without hitting LLM schema limits.
*   **[COST]** Massive reduction in baseline input tokens for simple file-editing tasks.
*   **[MODULARITY]** Encourages strict Separation of Concerns. Developers building new integrations must package them into cohesive `Toolkits`.

### Negative
*   **[LATENCY]** Introduces an N+1 LLM roundtrip problem at the start of a task. The LLM must "pause" to load tools before taking action. 
*   **[COMPLEXITY]** The execution loop must now handle dynamic schema mutation mid-conversation.

## 5. Implementation Guidance (For the Coder)

### A. Interface Boundaries (Hexagonal Architecture)
Create a clean interface for the registry rather than relying on global variables.

```go
package tools

// Toolkit represents a cohesive domain of tools.
type Toolkit struct {
    Name        string
    Description string
    Tools       []ToolDeclaration
}

// Registry manages all available capabilities.
type Registry interface {
    GetCoreTools() []ToolDeclaration
    GetToolkit(name string) (*Toolkit, error)
    ListAvailableToolkits() []string
}
```

### B. Session State Management
The active tools must be tied to the Session/History, not the global application state.

```go
package session

type ActiveSession struct {
    History       []Message
    LoadedDomains map[string]bool // Tracks which toolkits are currently in context
    // ...
}

// GetActiveToolSchema returns Core Tools + all tools from LoadedDomains
func (s *ActiveSession) GetActiveToolSchema(reg tools.Registry) []ToolDeclaration {
    // ...
}
```

### C. The Core Tool Definition
The new `load_toolkit` tool must clearly instruct the LLM on *why* and *how* to use it.

```json
{
  "name": "load_toolkit",
  "description": "CRITICAL: Use this to load specialized tools into your context. Available toolkits: ['git', 'k8s', 'ado', 'jira']. If a user asks to deploy to Kubernetes, you must call load_toolkit(names=['k8s']) first.",
  "parameters": {
    "type": "object",
    "properties": {
      "names": {
        "type": "array",
        "items": { "type": "string" }
      }
    },
    "required": ["names"]
  }
}
```
