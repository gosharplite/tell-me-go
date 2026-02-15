# Technical Specification: Multi-Provider LLM Integration

## 1. Strategic Goal
Transition `tell-me-go` from a Gemini-exclusive system into a provider-agnostic agent supporting the industry's leading reasoning models.

## 2. Target Model Registry
The system will provide native support for the following flagship models:

| Provider | Model ID | API Endpoint | Auth Type |
| :--- | :--- | :--- | :--- |
| **Google** | `gemini-3-flash-preview` | `https://aiplatform.googleapis.com/v1/...` | OAuth2/IAM |
| **OpenAI** | `gpt-5.2` | `https://api.openai.com/v1/chat/completions` | Bearer Token |
| **DeepSeek** | `deepseek-reasoner` | `https://api.deepseek.com/chat/completions` | Bearer Token |
| **Anthropic** | `claude-opus-4-6` | `https://api.anthropic.com/v1/messages` | API Key (x-api-key) |

## 3. Implementation Roadmap

### Phase 1: Domain & Configuration (Current)
- **Domain:** Update `internal/domain/llm.Part` to use a `Thought string` field.
    - **Status:** [COMPLETED]
    - **Compatibility:** For boolean-based providers (Gemini), a `"true"` string acts as a semantic marker indicating that the reasoning content is found within the `Text` field.
- **Config Registry:** Implement a formal `Providers` registry in the `Config` struct.
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
- **Environment Expansion:** The configuration loader must support recursive expansion of `${VAR}` tokens in YAML values for secure credential management. This expansion must happen at the raw YAML level (byte slice) before unmarshaling to ensure all fields (including nested provider keys) are expansion-capable.

### Phase 1.5: Factory Abstraction
- **Dynamic Factory:** Implement `internal/infrastructure/llm/factory.go` to abstract the instantiation of `google`, `openai`, and `anthropic` implementations.
- **Contract:** The factory must return an `LLMClient` interface based on the `Type` field in the active `LLMProvider` configuration, decoupling client creation from high-level CLI commands.

### Phase 2: OpenAI Compatible Infrastructure
- **Transport:** Implement `internal/infrastructure/llm/openai_compatible.go` using a manual HTTP client.
- **Mapping:** 
    - `gpt-5.2`: Map `reasoning_tokens` to internal `Thought`.
    - `deepseek-reasoner`: Map `reasoning_content` to internal `Thought`.

### Phase 2.5: Anthropic Infrastructure
- **Transport:** Implement `internal/infrastructure/llm/anthropic.go`.
- **Mapping:** Parse `response.content` where `type == "thinking"` and map it to internal `Thought`.
- **Headers:** Must include `anthropic-version: 2023-06-01`.

### Phase 3: Orchestration & Telemetry
- **Registry:** Update `internal/infrastructure/registry` for dynamic provider switching.
- **Pricing:** Update `internal/infrastructure/telemetry` to handle token-based billing for multiple providers.

### Phase 4: Resilience & Parity
- **Error Handling:** Unify HTTP/gRPC error classification in `resilient_client.go`.
- **E2E Testing:** Verify tool-calling and history management parity across all three models.

## 4. References
- [Google Vertex AI REST Reference](https://docs.cloud.google.com/vertex-ai/docs/reference/rest)
- [OpenAI Chat Completion Reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
- [DeepSeek API Reference](https://api-docs.deepseek.com/api/create-chat-completion)
- [Anthropic Messages API Reference](https://docs.anthropic.com/en/api/messages)
