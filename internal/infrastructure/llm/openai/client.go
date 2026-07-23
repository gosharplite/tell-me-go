// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

// imageURLBlock represents an image_url content part for vision-capable models.
type imageURLBlock struct {
	Type     string        `json:"type"`
	ImageURL imageURLValue `json:"image_url"`
}

// imageURLValue holds the URL payload for an image_url block.
type imageURLValue struct {
	URL string `json:"url"`
}

// videoURLBlock represents a video_url content part for vision-capable models.
type videoURLBlock struct {
	Type     string        `json:"type"`
	VideoURL videoURLValue `json:"video_url"`
}

// videoURLValue holds the URL payload for a video_url block.
type videoURLValue struct {
	URL string `json:"url"`
}

// turnAssets carries turn-scoped file-upload state: the binding from
// domain Parts to Kimi server file IDs, plus a list of uploaded file
// IDs for post-response cleanup. It is owned by a single SendChat call
// and never shared across goroutines.
type turnAssets struct {
	// bindings maps parts to their uploaded file IDs (ms:// references).
	bindings map[*llm.Part]string
	// uploaded is the ordered list of file IDs for cleanup.
	uploaded []string
}

func newTurnAssets() *turnAssets {
	return &turnAssets{
		bindings: make(map[*llm.Part]string),
	}
}

// resolveURL returns the image_url value for a part: ms://{file_id}
// if it was uploaded, or a base64 data URI otherwise. Nil-safe —
// returns a base64 data URI when ta is nil (no uploads occurred).
func (ta *turnAssets) resolveURL(p *llm.Part) string {
	if ta != nil {
		if fileID, ok := ta.bindings[p]; ok {
			return "ms://" + fileID
		}
	}
	return fmt.Sprintf("data:%s;base64,%s",
		p.InlineData.MIMEType,
		base64.StdEncoding.EncodeToString(p.InlineData.Data))
}

// release deletes all files uploaded during this turn. Best-effort;
// individual failures are logged. Uses a detached context so cleanup
// proceeds even if the caller's context is at deadline.
func (ta *turnAssets) release(ctx context.Context, c *client) {
	if len(ta.uploaded) == 0 {
		return
	}
	// Detach from the caller's context — cleanup is best-effort and
	// must not be gated by the response deadline.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	for _, id := range ta.uploaded {
		if err := c.deleteFile(cleanupCtx, id); err != nil {
			c.logger.Warn("cleanup_uploaded_file_failed",
				"file_id", id,
				"error", err.Error(),
			)
		}
	}
}

// collectHistoryParts gathers all non-system parts from history for
// asset preparation.
func collectHistoryParts(history []*llm.Content) []*llm.Part {
	var parts []*llm.Part
	for _, h := range history {
		if h.Role == "system" {
			continue
		}
		parts = append(parts, h.Parts...)
	}
	return parts
}

// applyPreparedParts writes prepared parts back into history, matching
// by index. Call after prepareMediaAssets has mutated parts in place.
func applyPreparedParts(history []*llm.Content, prepared []*llm.Part) {
	idx := 0
	for _, h := range history {
		if h.Role == "system" {
			continue
		}
		h.Parts = prepared[idx : idx+len(h.Parts)]
		idx += len(h.Parts)
	}
}

type message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content,omitempty"` // string, []requestContentBlock, or []any (mixed text+image)
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
	// AddMessage adds a message to the sink. content is either a string
	// (text-only) or []any (mixed text+image blocks for vision models).
	AddMessage(role string, content any, reasoning *string, toolCalls []toolCall)
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
	ta *turnAssets,
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

	// Separate media parts before classification — classifyParts only
	// handles text and function calls.
	mediaParts, textParts := extractMediaParts(otherParts)

	text, reasoning, toolCalls, err := c.classifyParts(textParts)
	if err != nil {
		return err
	}

	c.injectPersona(sink, personaInjected, role)

	var reasoningPtr *string
	if (c.capabilities.SupportsReasoningContent && role == "assistant") || (reasoning != "") {
		reasoningPtr = &reasoning
	}

	// Build content: array format when media present AND vision/video supported,
	// string format otherwise.
	var content any = text
	if (c.capabilities.SupportsVision || c.capabilities.SupportsVideo) && len(mediaParts) > 0 {
		// Media blocks are placed before text (media-first ordering). This is
		// intentional for the describe-this-image use case — interleaved
		// part ordering within a single history item is not preserved.
		blocks := mediaBlocks(mediaParts, ta)
		if text != "" {
			blocks = append(blocks, requestContentBlock{Type: "text", Text: text})
		}
		content = blocks
	}

	sink.AddMessage(role, content, reasoningPtr, toolCalls)

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

// extractMediaParts separates InlineData parts (image and video) from a part slice.
// Returns the media parts and the remaining non-media, non-tool-response parts.
func extractMediaParts(parts []*llm.Part) (media []*llm.Part, rest []*llm.Part) {
	for _, p := range parts {
		if p.InlineData != nil && len(p.InlineData.Data) > 0 {
			media = append(media, p)
		} else {
			rest = append(rest, p)
		}
	}
	return
}

// hydrateMediaAssets resolves AssetID references on parts whose
// InlineData.Data is nil (lazy-hydration pattern from session reload).
// It uses copy-on-write — the returned slice is independent of the
// input — to avoid mutating shared session history. Preserves MIMEType
// from any existing InlineData blob. Gated on SupportsVision || SupportsVideo;
// non-vision/non-video models return the input unchanged. Resolve errors are
// propagated: a corrupt asset store should fail the session loudly.
func (c *client) hydrateMediaAssets(ctx context.Context, parts []*llm.Part, resolver llm.AssetResolver) ([]*llm.Part, error) {
	if resolver == nil || (!c.capabilities.SupportsVision && !c.capabilities.SupportsVideo) {
		return parts, nil
	}
	out := parts
	cloned := false
	for i, p := range parts {
		if p.AssetID == "" || (p.InlineData != nil && len(p.InlineData.Data) > 0) {
			continue
		}
		data, err := resolver.Resolve(ctx, p.AssetID)
		if err != nil {
			return nil, fmt.Errorf("resolve asset %s: %w", p.AssetID, err)
		}
		// Copy-on-write: clone the full slice once on first mutation
		if !cloned {
			out = make([]*llm.Part, len(parts))
			copy(out, parts)
			cloned = true
		}
		// Clone the part to avoid mutating shared session history
		pc := *p
		if p.InlineData != nil {
			// Preserve MIMEType from the existing blob (set during AddImage)
			blob := *p.InlineData
			blob.Data = data
			pc.InlineData = &blob
		} else {
			pc.InlineData = &llm.Blob{Data: data}
		}
		out[i] = &pc
	}
	return out, nil
}

// prepareMediaAssets hydrates and uploads media parts (image and video)
// for a single turn. Returns the turnAssets with file bindings and the
// prepared (possibly cloned) part slice. Caller must call release after
// the LLM response.
func (c *client) prepareMediaAssets(ctx context.Context, parts []*llm.Part, resolver llm.AssetResolver) (*turnAssets, []*llm.Part, error) {
	ta := newTurnAssets()

	out, err := c.hydrateMediaAssets(ctx, parts, resolver)
	if err != nil {
		return ta, out, err
	}

	// Upload fresh media to Kimi file API
	if (!c.capabilities.SupportsVision && !c.capabilities.SupportsVideo) || !strings.Contains(c.baseURL, "api.moonshot.ai") {
		return ta, out, nil
	}
	for _, p := range out {
		if p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}
		if _, ok := ta.bindings[p]; ok {
			continue // already uploaded in this turn
		}
		ext := "png"
		if p.InlineData.MIMEType != "" {
			if parts := strings.Split(p.InlineData.MIMEType, "/"); len(parts) == 2 {
				ext = parts[1]
			}
		}
		purpose := "image"
		if strings.HasPrefix(p.InlineData.MIMEType, "video/") {
			purpose = "video"
		}
		filename := fmt.Sprintf("%s.%s", purpose, ext)
		fileID, err := c.uploadFile(ctx, p.InlineData.Data, filename, purpose)
		if err != nil {
			ta.release(ctx, c)
			return ta, out, fmt.Errorf("upload media: %w", err)
		}
		ta.bindings[p] = fileID
		ta.uploaded = append(ta.uploaded, fileID)
	}
	return ta, out, nil
}

// mediaBlocks converts InlineData parts to image_url or video_url content blocks.
// Uses turnAssets to resolve ms:// URLs for uploaded files; falls
// back to base64 data URIs for non-uploaded parts. Video MIME types
// (video/*) produce video_url blocks; all others produce image_url blocks.
func mediaBlocks(parts []*llm.Part, ta *turnAssets) []any {
	var blocks []any
	for _, p := range parts {
		if p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}
		url := ta.resolveURL(p)
		if strings.HasPrefix(p.InlineData.MIMEType, "video/") {
			blocks = append(blocks, videoURLBlock{
				Type:     "video_url",
				VideoURL: videoURLValue{URL: url},
			})
		} else {
			blocks = append(blocks, imageURLBlock{
				Type:     "image_url",
				ImageURL: imageURLValue{URL: url},
			})
		}
	}
	return blocks
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
				if c.capabilities.SupportsReasoningContent {
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
			return fmt.Errorf("%w: failed to unmarshal tool arguments %q: %w", llm.ErrTransient, truncate(argsStr, 200), err)
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
	return nil, llm.ErrNotImplemented
}

func (c *client) ExtractDocument(ctx context.Context, data []byte, filename string) (string, error) {
	return c.extractDocument(ctx, data, filename)
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

// newAuthenticatedRequest creates an HTTP request with all headers applied:
// custom provider headers (c.headers) and authenticator headers (by name,
// not by map iteration value). Used by both chat completions and file API
// endpoints for consistent authentication.
func (c *client) newAuthenticatedRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	// Custom provider headers (e.g. reasoning_effort)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	// Authenticator headers (e.g. Authorization, x-api-key)
	authReq := &auth.Request{Headers: make(map[string]string)}
	if err := c.authenticator.Apply(ctx, authReq); err != nil {
		return nil, err
	}
	for k, v := range authReq.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// truncate returns a string truncated to n characters with "..." appended.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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
