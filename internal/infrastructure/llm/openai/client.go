// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// client implements the llm.LLMClient interface for OpenAI-compatible APIs.
type client struct {
	httpClient     *http.Client
	transport      http.RoundTripper
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	capabilities   llm.Capabilities
	headers        map[string]string
	persona        string
	thinkingBudget int
	maxTokens      int
	logger         ports.Logger
	timeout        time.Duration
}

// openaiOption defines a functional option for configuring the OpenAI Client.
type openaiOption func(*client)

// WithHeaders sets the custom headers for the OpenAI Client.
func WithHeaders(headers map[string]string) openaiOption {
	return func(c *client) {
		c.headers = headers
	}
}

// WithPersona sets the initial persona instruction for the OpenAI Client.
func WithPersona(persona string) openaiOption {
	return func(c *client) {
		c.persona = persona
	}
}

// WithTimeout sets the HTTP timeout for the OpenAI Client.
func WithTimeout(timeout time.Duration) openaiOption {
	return func(c *client) {
		c.timeout = timeout
	}
}

// WithThinkingBudget sets the thinking budget for models that support it.
func WithThinkingBudget(budget int) openaiOption {
	return func(c *client) {
		c.thinkingBudget = budget
	}
}

// WithMaxTokens sets the per-request output-token cap, populating
// max_completion_tokens (or max_tokens for non-reasoning models such
// as DeepSeek Reasoner).
//
// Note: For backward compatibility with the pre-Task-H wiring,
// WithMaxTokens(0) falls back to WithThinkingBudget's value (not to
// defaultMaxTokens like Anthropic). This preserves byte-identical
// request payloads for deployments that previously relied on
// THINKING_BUDGET to drive max_completion_tokens. To force the package
// default, omit both options.
//
// Resolution order:
//  1. WithMaxTokens(N) where N > 0 → use N.
//  2. WithMaxTokens(0) or unset, WithThinkingBudget(M) where M > 0 → use M.
//  3. Both unset → use defaultMaxTokens (16384).
//
// See ADR-022 §References and the Task H commit message for history.
//
// Pinned by TestOpenAI_WithMaxTokens_Override,
// TestOpenAI_WithMaxTokens_ZeroFallsBackToThinkingBudget,
// TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault,
// and TestOpenAI_WithMaxTokens_DeepSeek_PopulatesMaxTokensField in
// maxtokens_test.go.
func WithMaxTokens(n int) openaiOption {
	return func(c *client) {
		if n > 0 {
			c.maxTokens = n
		}
	}
}

// WithLogger sets the logger for the OpenAI Client.
func WithLogger(l ports.Logger) openaiOption {
	return func(c *client) {
		c.logger = l
	}
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, opts ...openaiOption) *client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	c := &client{
		authenticator: authenticator,
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		model:         model,
		capabilities:  llm.ResolveCapabilities(model, strings.TrimSuffix(baseURL, "/")),
		logger:        &ports.NoOpLogger{},
	}

	for _, opt := range opts {
		opt(c)
	}

	// Baseline defense against hung connections
	if c.timeout == 0 {
		c.timeout = 60 * time.Second
	}

	var tr http.RoundTripper
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = defaultTransport.Clone()
	} else {
		tr = http.DefaultTransport
	}

	c.transport = tr
	c.httpClient = &http.Client{Timeout: c.timeout, Transport: tr}

	return c
}

type chatRequest struct {
	Model               string           `json:"model"`
	Messages            []message        `json:"messages,omitempty"`
	Input               []historyItem    `json:"input,omitempty"`
	Tools               []tool           `json:"tools,omitempty"`
	MaxTokens           int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	MaxOutputTokens     int              `json:"max_output_tokens,omitempty"` // NEW: for /responses endpoint
	Reasoning           *reasoningConfig `json:"reasoning,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
	// ChatTemplateKwargs carries non-standard template parameters required
	// by certain transports. Used to enable thinking mode on Vertex AI's
	// deepseek-ai/deepseek-v3.2-maas, which silently ignores the standard
	// "thinking" field. See Capabilities.RequiresVertexThinkingKwargs.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type historyItem struct {
	Type      string                `json:"type"`
	Role      *string               `json:"role,omitempty"`
	Content   []requestContentBlock `json:"content,omitempty"`
	CallID    *string               `json:"call_id,omitempty"`
	Name      *string               `json:"name,omitempty"`      // For function_call
	Arguments *string               `json:"arguments,omitempty"` // For function_call
	Output    *string               `json:"output,omitempty"`    // For function_call_output
}

type reasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type responsesAPIResponse struct {
	ID     string               `json:"id"`
	Output []responseOutputItem `json:"output"`
	Usage  usage                `json:"usage"`
}

type responseOutputItem struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"` // For top-level calls
	// Nested Message format
	Message *struct {
		Role      string         `json:"role"`
		Content   []contentBlock `json:"content"`
		ToolCalls []toolCall     `json:"tool_calls"`
	} `json:"message,omitempty"`
	// Direct Content Block format (fallback for heterogeneous items)
	Role       string         `json:"role"`
	Content    []contentBlock `json:"content"`
	ToolCalls  []toolCall     `json:"tool_calls"`
	Text       interface{}    `json:"text"`
	InputText  string         `json:"input_text"`
	OutputText string         `json:"output_text"`
	Thought    string         `json:"thought"`
	Reasoning  string         `json:"reasoning"`
	Refusal    string         `json:"refusal"`
	Usage      *usage         `json:"usage"`
	// Top-level Call support
	Function  *functionCall `json:"function,omitempty"`
	Name      string        `json:"name,omitempty"`      // Flattened fallback
	Arguments string        `json:"arguments,omitempty"` // Flattened fallback
}

type contentBlock struct {
	Type       string                 `json:"type"`
	Text       interface{}            `json:"text,omitempty"`
	InputText  string                 `json:"input_text,omitempty"`
	OutputText string                 `json:"output_text,omitempty"`
	Thought    string                 `json:"thought,omitempty"`
	Reasoning  string                 `json:"reasoning,omitempty"` // Support 'reasoning' key
	Refusal    string                 `json:"refusal,omitempty"`   // Support model refusals
	ID         string                 `json:"id,omitempty"`        // For 'tool_use' blocks
	Name       string                 `json:"name,omitempty"`      // For 'tool_use' blocks
	Input      map[string]interface{} `json:"input,omitempty"`     // For 'tool_use' blocks
}

type requestContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"` // Required field for input_text / output_text
}

type message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content,omitempty"` // string or []requestContentBlock
	ToolCalls        []toolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	ReasoningContent *string     `json:"reasoning_content,omitempty"`
}

type tool struct {
	Type        string               `json:"type"`
	Name        string               `json:"name,omitempty"`
	Description string               `json:"description,omitempty"`
	Parameters  *schema              `json:"parameters,omitempty"`
	Function    *functionDeclaration `json:"function,omitempty"`
}

type functionDeclaration struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Parameters  *schema `json:"parameters,omitempty"`
}

type schema struct {
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Items       *schema            `json:"items,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	ID      string   `json:"id"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	InputTokens      int32 `json:"input_tokens,omitempty"`  // Alternative
	OutputTokens     int32 `json:"output_tokens,omitempty"` // Alternative
	TotalTokens      int32 `json:"total_tokens"`
	// OpenAI standard
	PromptTokensDetails     *promptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
	// DeepSeek standard
	PromptCacheHitTokens  int32 `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int32 `json:"prompt_cache_miss_tokens,omitempty"`
	// Vertex AI standard
	ExtraProperties *extraProperties `json:"extra_properties,omitempty"`
}

type extraProperties struct {
	Google *googleProperties `json:"google,omitempty"`
}

type googleProperties struct {
	TrafficType string `json:"traffic_type,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int32 `json:"cached_tokens"`
}

type completionTokensDetails struct {
	ReasoningTokens int32 `json:"reasoning_tokens"`
}

type openaiSink interface {
	AddMessage(role, text string, reasoning *string, toolCalls []toolCall)
	AddToolResponse(id, response string)
}

func (c *client) maybeInjectInitialPersona(sink openaiSink) (personaInjected bool) {
	if c.persona != "" && !c.capabilities.UseDeveloperRole { // Non-OpenAI reasoners use 'system' at start
		sink.AddMessage("system", c.persona, nil, nil)
		return true
	}
	return false
}

func (c *client) appendMessagesFromHistoryItem(
	ctx context.Context,
	sink openaiSink,
	h *llm.Content,
	resolver llm.AssetResolver,
	personaInjected *bool,
) error {
	role := normalizeRole(h.Role)

	toolResponseParts, otherParts := partitionParts(h.Parts)

	if err := c.appendToolResponseMessages(sink, toolResponseParts); err != nil {
		return err
	}

	if len(otherParts) == 0 {
		return nil
	}

	text, reasoning, toolCalls, err := c.classifyParts(otherParts)
	if err != nil {
		return err
	}

	c.injectPersona(sink, personaInjected, role)

	var reasoningPtr *string
	if (c.capabilities.IsDeepSeek && role == "assistant") || (reasoning != "") {
		reasoningPtr = &reasoning
	}

	sink.AddMessage(role, text, reasoningPtr, toolCalls)

	return nil
}

func normalizeRole(role string) string {
	if role == "model" {
		return "assistant"
	}
	return role
}

func partitionParts(parts []*llm.Part) (toolResponseParts []*llm.Part, otherParts []*llm.Part) {
	toolResponseParts = make([]*llm.Part, 0, len(parts))
	otherParts = make([]*llm.Part, 0, len(parts))
	for _, p := range parts {
		if p.FunctionResponse != nil {
			toolResponseParts = append(toolResponseParts, p)
		} else {
			otherParts = append(otherParts, p)
		}
	}
	return
}

func (c *client) appendToolResponseMessages(sink openaiSink, toolResponseParts []*llm.Part) error {
	for _, p := range toolResponseParts {
		// Fail fast if tool response has an empty ID - it violates protocol and indicates state corruption
		if p.FunctionResponse.ID == "" {
			c.logger.Error("Encountered tool response with empty ID during serialization", "tool_name", p.FunctionResponse.Name)
			return fmt.Errorf("invalid tool payload: missing ID for tool response '%s'", p.FunctionResponse.Name)
		}
		res, err := marshalResponse(p.FunctionResponse.Response)
		if err != nil {
			return fmt.Errorf("failed to marshal tool response: %w", err)
		}

		sink.AddToolResponse(p.FunctionResponse.ID, res)
	}
	return nil
}

// classifyParts categorizes different parts of a message.
// It returns an error if tool arguments cannot be marshalled to JSON.
func (c *client) classifyParts(parts []*llm.Part) (text string, reasoning string, toolCalls []toolCall, err error) {
	var textParts []string
	var reasoningParts []string
	for _, p := range parts {
		if p.FunctionCall != nil {
			// Fail fast if tool call has an empty ID - it violates protocol and indicates state corruption
			if p.FunctionCall.ID == "" {
				c.logger.Error("Encountered tool call with empty ID during serialization", "tool_name", p.FunctionCall.Name)
				return "", "", nil, fmt.Errorf("invalid tool payload: missing ID for tool call '%s'", p.FunctionCall.Name)
			}
			args, err := marshalArgs(p.FunctionCall.Args)
			if err != nil {
				return "", "", nil, fmt.Errorf("failed to marshal tool arguments: %w", err)
			}
			toolCalls = append(toolCalls, toolCall{
				ID:   p.FunctionCall.ID,
				Type: "function",
				Function: functionCall{
					Name:      p.FunctionCall.Name,
					Arguments: args,
				},
			})
		} else if p.Text != "" {
			if p.IsThought {
				if c.capabilities.IsDeepSeek {
					reasoningParts = append(reasoningParts, p.Text)
				} else {
					textParts = append(textParts, fmt.Sprintf("<thought>\n%s\n</thought>", p.Text))
				}
			} else {
				textParts = append(textParts, p.Text)
			}
		}
	}
	return strings.Join(textParts, "\n"), strings.Join(reasoningParts, ""), toolCalls, nil
}

func (c *client) injectPersona(sink openaiSink, personaInjected *bool, role string) {
	if role != "user" || *personaInjected || c.persona == "" {
		return
	}

	if c.capabilities.UseDeveloperRole {
		sink.AddMessage("developer", c.persona, nil, nil)
		*personaInjected = true
	}
}

// resolveThinkingTokens extracts reasoning/thinking token count from completion details.
func (c *client) resolveThinkingTokens(u usage) int32 {
	if u.CompletionTokensDetails != nil {
		return u.CompletionTokensDetails.ReasoningTokens
	}
	return 0
}

func (c *client) parseResponseContent(rawContent interface{}, content *llm.Content) {
	switch v := rawContent.(type) {
	case string:
		if v != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: v})
		}
	case []interface{}:
		for _, part := range v {
			c.parseContentPart(part, content)
		}
	}
}

func (c *client) parseContentPart(part interface{}, content *llm.Content) {
	m, ok := part.(map[string]interface{})
	if !ok {
		return
	}

	contentType, _ := m["type"].(string)
	switch contentType {
	case "text":
		if txt, ok := m["text"].(string); ok && txt != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: txt})
		}
	case "thought", "reasoning":
		if thought, ok := m[contentType].(string); ok && thought != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: thought, IsThought: true})
		}
	}
}

func (c *client) appendToolCall(content *llm.Content, id, name, argsStr string) error {
	var args map[string]interface{}
	if argsStr != "" && argsStr != "{}" {
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			return fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
	}
	if name == "" && args == nil {
		return nil
	}
	content.Parts = append(content.Parts, &llm.Part{
		FunctionCall: &llm.FunctionCall{
			ID:   id,
			Name: name,
			Args: args,
		},
	})
	return nil
}

// parseResponseToolCalls extracts tool calls from the API response.
// It returns an error if tool arguments cannot be unmarshalled from JSON.
func (c *client) parseResponseToolCalls(toolCalls []toolCall, content *llm.Content) error {
	for _, tc := range toolCalls {
		if err := c.appendToolCall(content, tc.ID, tc.Function.Name, tc.Function.Arguments); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, fmt.Errorf("GenerateImages not implemented for OpenAI")
}

func (c *client) RefreshAuth() error {
	c.authenticator.Invalidate()
	return nil
}

type idleConnectionCloser interface {
	CloseIdleConnections()
}

// ResetConnections flushes the underlying connection pool to ensure a fresh network path.
func (c *client) ResetConnections() {
	if closer, ok := c.transport.(idleConnectionCloser); ok {
		closer.CloseIdleConnections()
	}
}

// marshalArgs converts tool arguments map to a JSON string.
// It returns an error if JSON marshalling fails.
func marshalArgs(args map[string]interface{}) (string, error) {
	if args == nil {
		return "{}", nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// marshalResponse converts tool response map to a JSON string.
// It returns an error if JSON marshalling fails.
func marshalResponse(res map[string]interface{}) (string, error) {
	if res == nil {
		return "", nil
	}
	// Typically we want the 'result' field if it exists, otherwise the whole thing
	if val, ok := res["result"].(string); ok {
		return val, nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
