# LLM Provider Error Testing Pattern

Canonical table-driven pattern for testing error handling across LLM provider clients.
Established in Issue #873. To be reused in follow-up distributed error-handling work.

## Pattern Overview

Every LLM provider client has a single entry point (`SendChat`) that can fail in
three layers:

1. **Transport layer** — network errors, DNS failures, connection refused, timeouts
2. **HTTP layer** — non-200 status codes with structured/unstructured response bodies
3. **Application layer** — malformed JSON, empty responses, tool-call parse failures

Each layer requires a specific testing technique.

## Canonical Test Structure

```go
func Test<Provider>_ErrorHandling(t *testing.T) {
    tests := []struct {
        name              string
        statusCode        int
        response          string
        wantErrContains   string
        wantAPIError      bool
        wantAPIErrStatus  int
        wantSentinel      error
    }{
        // Dimension 1: HTTP Status Codes
        // ... 400, 401, 403, 408, 429, 499, 500, 502, 503 ...
        // Dimension 2: Provider-Specific Error Shapes
        // ... overloaded_error, permission_error, etc ...
        // Dimension 3: Malformed Response Bodies
        // ... malformed JSON, empty body, non-JSON body ...
        // Dimension 4: Application-Layer Errors
        // ... empty choices, invalid tool args ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Arrange: httptest server + client
            // Act: SendChat
            // Assert: error type + Classify chain
        })
    }
}
```

## Assertion Template (Status Code Rows)

Every status-code row must verify **three** assertions:

```go
// 1. Error wraps *llmerr.APIError with correct status
var apiErr *llmerr.APIError
require.True(t, errors.As(err, &apiErr), "expected *llmerr.APIError")
require.Equal(t, tt.wantAPIErrStatus, apiErr.Status)

// 2. Body is captured
require.Contains(t, apiErr.Body, tt.wantBodyContains)

// 3. llmerr.Classify maps to correct domain sentinel
classified := llmerr.Classify(apiErr)
require.True(t, errors.Is(classified, tt.wantSentinel),
    "Classify: got %v, want %v", classified, tt.wantSentinel)
```

## Provider-Specific Adaptations

### OpenAI / Anthropic (Raw HTTP)

Use `httptest.NewServer` with a handler that writes the desired status code and
response body, then construct the client pointing at the test server URL.

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(tt.statusCode)
    _, _ = w.Write([]byte(tt.response))
}))
defer server.Close()
client := NewClient(server.URL, "model", authenticator)
```

### Gemini (GenAI SDK)

Two approaches:

1. **HTTP-level** (`TestSendChat_ClassifyError_Integration`): Same `httptest` pattern
   as OpenAI/Anthropic. The SDK translates HTTP errors into `genai.APIError` strings,
   which `classifyError` → `llmerr.Classify` matches via `classifyString` regex/keyword.

2. **SDK-level** (`TestSendChat_SDKError_Direct`): Inject a `newGenaiClient` factory
   that returns a client wired to fail. This tests the `initSDK` → `classifyError`
   pipeline without the HTTP layer.

## Status Code → Domain Sentinel Mapping

| HTTP Status | Domain Sentinel | Classification |
|-------------|----------------|----------------|
| 401 | `llm.ErrAuth` | Authentication failure |
| 403 | `llm.ErrTerminal` | Permission denied |
| 400 | `llm.ErrTerminal` | Invalid request |
| 408 | `llm.ErrTransient` | Request timeout |
| 429 | `llm.ErrRateLimit` | Rate limited |
| 499 | `llm.ErrTransient` | Client closed request |
| 500 | `llm.ErrTransient` | Internal server error |
| 502 | `llm.ErrTransient` | Bad gateway |
| 503 | `llm.ErrTransient` | Service unavailable |
| 529 | `llm.ErrTransient` | Overloaded (Anthropic-specific) |

## Test Coverage Checklist

When adding a new provider or extending an existing one:

- [ ] All 9 HTTP status codes (400–503) tested
- [ ] Empty response body variant for at least one 5xx code
- [ ] Non-JSON response body variant for at least one 5xx code
- [ ] Provider-specific error shapes (overloaded_error, permission_error, etc.)
- [ ] Malformed JSON on 200 response
- [ ] Transport-layer errors (timeout, connection refused, pre-cancelled context)
- [ ] Double-failure path (non-200 + body read failure)
- [ ] `llmerr.Classify` verified on every status-code row
- [ ] SDK-level error injection (for SDK-based providers)

## Related Files

- `internal/infrastructure/llm/llmerr/errors.go` — Classify, APIError, regex patterns
- `internal/infrastructure/llm/llmerr/errors_test.go` — Classify unit tests (classifier in isolation)
- `internal/infrastructure/llm/openai/client_chat_test.go` — `TestSendChat_ErrorHandling`
- `internal/infrastructure/llm/anthropic/client_test.go` — `TestSendChat_ErrorHandling`
- `internal/infrastructure/llm/gemini/gemini_test.go` — `TestSendChat_ClassifyError_Integration`, `TestClassifyError`, `TestSendChat_SDKError_Direct`
