# ADR-001: Hybrid LLM Infrastructure Strategy

## Status
Accepted

## Context
`tell-me-go` currently relies exclusively on the `google.golang.org/genai` SDK for interaction with Gemini models. To remain competitive and versatile, the system must support next-generation models like OpenAI's `gpt-5.2`, DeepSeek's `deepseek-reasoner`, and Anthropic's Claude Opus 4.6.

## Decision
We will adopt a **Hybrid Infrastructure Strategy**:
1. **Gemini Implementation:** Retain the official Google `genai` SDK. It handles the complex OAuth2/IAM authentication and multimodal data hydration required by Vertex AI.
2. **OpenAI & DeepSeek Implementation:** Implement a **Manual HTTP Client** within the project (`internal/infrastructure/llm/openai_compatible.go`). 
    * Both providers follow the OpenAI Chat Completion v1 standard.
    * A manual implementation avoids adding heavy external SDK dependencies.
    * It allows for precise parsing of reasoning fields (`reasoning_content` for DeepSeek, `reasoning_tokens` for GPT-5.2).
3. **Anthropic Implementation:** Implement a dedicated **Manual HTTP Client** for the Anthropic Messages API. Unlike DeepSeek, Claude uses a unique content-block structure that requires a specific JSON transformer to map "thinking" blocks to our internal domain.

## Consequences
- **Positive:** Zero-dependency integration for OpenAI-compatible providers.
- **Positive:** Future-proof support for any provider following the OpenAI standard (e.g., Groq, Ollama).
- **Positive:** Unified internal "Thinking" model across all providers.
- **Positive:** Native support for Claude's "Extended Thinking" features without relying on third-party proxy translations.
- **Negative:** Manual maintenance of the HTTP transport and JSON parsing logic for the OpenAI standard.
