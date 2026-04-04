# ADR-015: Cross‑Platform Timing Guarantees and HTTP‑Streaming Duration Measurement

## Status
Accepted

## Context
### Problem Statement
The system experienced implausible token‑throughput values (>300 tokens/sec) for DeepSeek and other OpenAI‑compatible providers. Root‑cause analysis revealed an HTTP‑streaming timing measurement bug where duration was measured after `httpClient.Do()` returned (time‑to‑first‑byte) but before reading the streaming response body. This bug manifested differently across platforms due to network‑stack variances.

### Symptoms
- **Implausible Latency Measurements**: Streaming LLM responses were reported with unrealistically low latency (e.g., 0.29 s for 89 tokens).
- **Inaccurate Token‑Throughput**: Reported throughput exceeded hardware‑plausible limits (>300 tokens/sec), indicating measurement error rather than actual performance.
- **Platform‑Specific Variance**: macOS vs Linux TCP/HTTP stack differences amplified the measurement error, causing inconsistent benchmarking results across deployment environments.
- **Business Impact**: Incorrect cost calculations, misleading performance monitoring, and unreliable cross‑platform benchmarks.

### Root Cause
The HTTP client timing originally captured only the **time‑to‑first‑byte (TTFB)** — the interval from request dispatch until the HTTP response headers arrived. For streaming/chunked responses, the body transfer time (which can be substantial for large token counts) was omitted from the duration measurement. Consequently, latency appeared artificially low, and token‑throughput values became inflated beyond physical limits.

## Decision
A three‑part solution was implemented to ensure accurate cross‑platform timing and reliable performance metrics.

### Part A: Fix HTTP‑Streaming Duration Measurement
**Location**: `internal/infrastructure/llm/openai/client.go` and `internal/infrastructure/llm/anthropic/client.go` – `SendChat` methods

**Changes**:
1. **Measure Total Transaction Time**: Duration is now measured **after** the JSON stream decoding (`json.NewDecoder(resp.Body).Decode(&resp)`) completes, capturing the full HTTP transaction (headers + streaming body transfer).
2. **Explicit Timing Breakdown**: Introduced separate timing variables:
   - `ttfb` – time‑to‑first‑byte (headers received)
   - `bodyReadTime` – time spent consuming the response stream during JSON decoding
   - `totalDuration` – sum of both, used as the official LLM response duration
3. **Preserve Streaming Scalability**: Retained `json.NewDecoder` to parse responses concurrently as they arrive over the network, avoiding `io.ReadAll` O(N) memory allocations for large tool-call payloads on success paths.

### Part B: Throughput Validation
**Location**: `internal/agent/turn_engine.go` – `inferenceStep.validateMetrics` method

**Changes**:
1. **Plausibility Constant**: Added `maxPlausibleTokensPerSecond = 5000` constant, representing a hardware‑sanity‑check upper bound that far exceeds current and near‑future LLM inference speeds (modern high‑performance engines can reach 150‑800+ TPS).
2. **Runtime Validation**: `validateMetrics` calculates token‑throughput (`response_tokens / duration`) and logs a structured warning when the value exceeds the hardware‑plausible limit.
3. **Non‑Blocking Design**: The warning does not interrupt execution, preserving forward compatibility while alerting engineers to potential measurement bugs.

### Part C: Platform‑Aware Timing Diagnostics
**Location**: LLM clients (`openai/client.go`, `anthropic/client.go`)

**Changes**:
1. **Platform Detection**: Added `runtime.GOOS` detection to all timing logs, enabling correlation of timing anomalies with specific operating systems.
2. **Structured Logging**: Introduced two new debug‑level log events:
   - `http_timing_breakdown` – reports `ttfb_ms`, `body_read_ms`, `total_ms`, `platform`, `provider`, `model`, and `endpoint`
   - `token_throughput` – reports `response_tokens`, `duration_sec`, `tokens_per_sec`, `cached_tokens`, and `platform`
3. **Cross‑Platform Debugging**: Engineers can now isolate DNS/TCP/TLS variance from streaming‑transfer variance by comparing TTFB vs body‑read times across macOS and Linux environments.

## Consequences
### Positive
- **Accurate Costing**: Duration now reflects true end‑to‑end LLM response time, ensuring correct token‑based cost attribution.
- **Cross‑Platform Debugging**: Platform‑specific network‑stack differences can be diagnosed via structured timing breakdowns.
- **Runtime Detection**: Implausible throughput triggers warnings, acting as a canary for measurement bugs or platform‑specific timing anomalies.
- **Non‑Invasive**: Diagnostics are debug‑level only (`LOG_LEVEL=debug`) and do not affect production performance.

### Negative
- **Log Volume**: Additional debug logs may increase storage/bandwidth in production environments (mitigated by default‑off logging).
- **Complexity**: More timing‑related code in LLM clients requires careful maintenance and testing across platforms.

### Neutral
- **No API Changes**: All external interfaces (LLM client API, metrics structure, event bus) remain unchanged.
- **Backward Compatible**: Existing metrics and cost‑tracking continue to work; the fix only corrects the underlying measurement.

## Implementation References
- `internal/infrastructure/llm/openai/client.go` – `SendChat` method (timing breakdown, platform logging)
- `internal/infrastructure/llm/anthropic/client.go` – `SendChat` method (timing breakdown, platform logging)  
- `internal/agent/turn_engine.go` – `validateMetrics` method and `maxPlausibleTokensPerSecond` constant

## Related ADRs
- **ADR-001**: Hybrid LLM Infrastructure Strategy – established the multi‑provider architecture where timing consistency is critical.
- **ADR-002**: Standardize Tool Execution Concurrency, Timeouts, and Context Propagation – established patterns for cross‑cutting observability.

## Verification
The fix can be verified by:
1. Enabling debug logging (`LOG_LEVEL=debug`) and observing `http_timing_breakdown` and `token_throughput` events.
2. Comparing TTFB vs total duration for streaming responses (body‑read time should be positive).
3. Confirming token‑throughput values remain below the hardware sanity limit (5000 tokens/sec) for realistic workloads.
4. Testing across macOS and Linux to ensure consistent timing measurements.

---
*Last Updated: 2026‑04‑04*