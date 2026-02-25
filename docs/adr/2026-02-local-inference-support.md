## ADR-005: Local Inference Support

**Status:** Accepted
**Date:** 2026-02-14

### Context
Users increasingly want to use `tell-me-go` with locally hosted Large Language Models (LLMs) to ensure privacy, reduce latency, and eliminate per-token costs. Popular local model runners include Ollama, vLLM, and Llama.cpp.

### Decision
We will support local inference by leveraging the existing OpenAI-compatible transport layer.

1.  **Authentication**: Local models typically do not require authentication. We will introduce a `NoOpAuth` implementation of the `Authenticator` interface that performs no header modifications.
2.  **Provider Types**: We will recognize `local` and `ollama` as valid provider types in the configuration.
3.  **Routing**: The `llm.NewClient` factory will route these provider types to the `openai.Client` implementation.
4.  **Bypass Validation**: The factory will bypass the "API key required" check for these specific provider types.

### Consequences
- **Privacy**: Users can use the CLI without sending data to third-party cloud providers.
- **Flexibility**: Any tool exposing an OpenAI-compatible endpoint can be used with `tell-me-go`.
- **Maintenance**: Reusing the `openai.Client` minimizes code duplication and leverages existing streaming/error handling logic.
