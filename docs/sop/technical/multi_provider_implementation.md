# Technical Specification: Multi-Provider LLM Integration

## 1. Strategic Goal
Provide a robust, provider-agnostic agent framework that natively supports flagship reasoning models from Google, OpenAI, Anthropic, and DeepSeek.

## 2. Target Model Registry
The system provides native support for the following flagship models (as of Feb 2026):

| Provider | Model ID | API Endpoint | Auth Type |
| :--- | :--- | :--- | :--- |
| **Google** | `gemini-3-flash-preview` | `https://aiplatform.googleapis.com/v1/...` | OAuth2/IAM |
| **OpenAI** | `gpt-5.2` | `https://api.openai.com/v1/chat/completions` | Bearer Token |
| **DeepSeek** | `deepseek-reasoner` | `https://api.deepseek.com/chat/completions` | Bearer Token |
| **Anthropic** | `claude-opus-4-6` | `https://api.anthropic.com/v1/messages` | API Key (x-api-key) |

## 3. Implementation Roadmap

### Phase 1: Domain & Configuration (Completed)
- **Domain:** Update `internal/domain/llm.Part` to use a `Thought string` field.
    - **Status:** [x]
    - **Compatibility:** For boolean-based providers (Gemini), a `"true"` string acts as a semantic marker indicating that the reasoning content is found within the `Text` field.
- **Config Registry:** Implement a formal `Providers` registry in the `Config` struct.
    - **Status:** [x]
    - **LLMProvider Schema:**
        ```go
        type LLMProvider struct {
            Type     string `yaml:"TYPE"`     // google, openai, deepseek, anthropic
            Model    string `yaml:"MODEL"`    // Model ID
            URL      string `yaml:"URL"`      // Base URL or Endpoint
            APIKey   string `yaml:"API_KEY"`  // Sensitive credential
            Thinking int    `yaml:"THINKING"` // Thinking budget/tokens
        }
        ```
    - **Selected Provider:** The `Config` struct must include a `SELECTED_PROVIDER` field to determine which key from the `PROVIDERS` map is active.
- **Environment Expansion:** The configuration loader must support recursive expansion of `${VAR}` tokens in YAML values for secure credential management.
    - **Status:** [x]

### Phase 1.5: Factory Abstraction (Completed)
- **Dynamic Factory:** Implement `internal/infrastructure/llm/factory.go` to abstract the instantiation of `google`, `openai`, and `anthropic` implementations.
- **Contract:** The factory must return an `LLMClient` interface based on the `Type` field in the active `LLMProvider` configuration, decoupling client creation from high-level CLI commands.
- **Status:** [x]

### Phase 2: OpenAI Compatible Infrastructure (Completed)
- **Transport:** Implement `internal/infrastructure/llm/openai/` using a manual HTTP client.
- **Mapping:** 
    - `gpt-5.2`: Map `reasoning_tokens` to internal `Thought`.
    - `deepseek-reasoner`: Map `reasoning_content` to internal `Thought`.
- **Streaming:** SSE support for real-time deltas.
- **Status:** [x]

### Phase 2.5: Anthropic Infrastructure (Completed)
- **Transport:** Implement `internal/infrastructure/llm/anthropic/`.
- **Mapping:** Parse `response.content` where `type == "thinking"` and map it to internal `Thought`.
- **Headers:** Must include `anthropic-version: 2023-06-01`.
- **Streaming:** SSE support for `content_block_delta` and `message_delta`.
- **Status:** [x]

### Phase 3: Orchestration & Telemetry (Completed)
- **Registry:** Implementation of `LLMProvider` registry and `SelectedProvider` logic in `internal/domain/config`.
- **Pricing:** Telemetry support for token-based billing, including specialized "Thinking Rates" for reasoning models.
- **Status:** [x]

### Phase 4: Release & Optimization (In Progress)
- **Resilience:** Unified error classification (Transient vs. Terminal) implemented via `internal/infrastructure/llm/llmerr`.
- **E2E Testing:** Validation of tool-calling and history management parity across Google, OpenAI, and Anthropic providers.
- **Performance:** Benchmarking token throughput and latency for 2026 flagship models.
- **Status:** [In Progress]

## 4. References
- [Google Vertex AI REST Reference](https://docs.cloud.google.com/vertex-ai/docs/reference/rest)
- [OpenAI Chat Completion Reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
- [DeepSeek API Reference](https://api-docs.deepseek.com/api/create-chat-completion)
- [Anthropic Messages API Reference](https://docs.anthropic.com/en/api/messages)
