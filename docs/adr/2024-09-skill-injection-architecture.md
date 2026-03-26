# ADR-005: Skill Injection Architecture

## Status
Accepted

## Context
The system currently maintains a library of idiomatic Go patterns and testing practices within the `docs/skills/` directory. However, this knowledge is statically defined within the system prompt or manually managed. This approach is suboptimal as it:
- **Wastes Context Window Tokens**: Including the entire skill library in every LLM request consumes a significant portion of the context window with potentially irrelevant information.
- **Misses Contextual Relevance**: Statically defined prompts cannot adapt to the specific needs of a given user request, missing opportunities to provide highly targeted guidance.

## Decision
We will implement a dynamic **Skill Injection Architecture** to optimize how Go-specific knowledge is shared with the LLM. 

The architecture will consist of two primary components:
1.  **In-Memory Skill Registry**: A service that loads all Markdown skill definitions from `docs/skills/` into an in-memory cache during application startup.
2.  **Token-Aware `SkillSelector`**: An algorithm integrated into the `ContextPreparationService` that dynamically selects the most relevant skills for a given request. This selector will balance relevance with the remaining context window capacity to ensure optimal token utilization.

## Consequences

### Positive
- **Reduced Latency**: By caching skill definitions at boot, we entirely bypass disk I/O latency in the LLM execution hot path (`Chat()`).
- **Context Optimization**: Only the most relevant skills are injected, maximizing the available context window for conversation history and tool outputs.
- **Enhanced Accuracy**: Providing the LLM with focused, relevant patterns improves the quality and idiomaticity of the generated Go code.

### Negative / Effort
- **Memory Overhead**: We must incur a memory overhead to maintain the cache of Markdown files in RAM. Given the current and projected size of the skill library, this is considered an acceptable trade-off for the performance gains.
- **Startup Complexity**: The application's boot sequence will require additional logic to crawl, parse, and register skills.
- **Selection Logic Maintenance**: The `SkillSelector` algorithm will require ongoing tuning to ensure it remains effective as the skill library and project complexity grow.
