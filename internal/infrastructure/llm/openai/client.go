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
	authcontract "github.com/gosharplite/tell-me-go/internal/infrastructure/auth/contract"
)

// client implements the llm.LLMClient interface for OpenAI-compatible APIs.
type client struct {
	httpClient         *http.Client
	transport          http.RoundTripper
	authenticator      authcontract.Authenticator
	baseURL            string
	model              string
	capabilities       llm.Capabilities
	headers            map[string]string
	persona            string
	thinkingBudget     int
	maxTokens          int
	thinkingEnabled    bool   // thinking toggle value; meaningless unless thinkingEnabledSet is true
	thinkingEnabledSet bool   // true when WithThinkingEnabled was called (tri-state: unset vs explicit false)
	userID             string // DeepSeek user_id for isolation
	logger             ports.Logger
	timeout            time.Duration
}

// Option defines a functional option for configuring the OpenAI Client.
type Option func(*client)

// WithHeaders sets the custom headers for the OpenAI Client.
func WithHeaders(headers map[string]string) Option {
	return func(c *client) {
		c.headers = headers
	}
}

// WithPersona sets the initial persona instruction for the OpenAI Client.
func WithPersona(persona string) Option {
	return func(c *client) {
		c.persona = persona
	}
}

// WithTimeout sets the HTTP timeout for the OpenAI Client.
func WithTimeout(timeout time.Duration) Option {
	return func(c *client) {
		c.timeout = timeout
	}
}

// WithThinkingBudget sets the thinking budget for models that support it.
func WithThinkingBudget(budget int) Option {
	return func(c *client) {
		c.thinkingBudget = budget
	}
}

// WithThinkingEnabled controls the DeepSeek/Kimi thinking-mode toggle.
// When true, the request includes {"thinking": {"type": "enabled"}}.
// When false, includes {"thinking": {"type": "disabled"}}.
// When not called at all, the field is omitted from the wire, preserving
// the provider's default. Only emitted for models where
// SupportsThinkingToggle is true.
func WithThinkingEnabled(enabled bool) Option {
	return func(c *client) {
		c.thinkingEnabled = enabled
		c.thinkingEnabledSet = true
	}
}

// WithUserID sets the DeepSeek user_id for content safety isolation,
// KVCache isolation, and scheduling isolation. The value is validated
// at config load ([a-zA-Z0-9\-_]+, max 512). Do not include PII.
// Only emitted for models where SupportsThinkingToggle is true.
func WithUserID(id string) Option {
	return func(c *client) {
		c.userID = id
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
func WithMaxTokens(n int) Option {
	return func(c *client) {
		if n > 0 {
			c.maxTokens = n
		}
	}
}

// WithLogger sets the logger for the OpenAI Client.
func WithLogger(l ports.Logger) Option {
	return func(c *client) {
		c.logger = l
	}
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(baseURL, model string, authenticator authcontract.Authenticator, opts ...Option) *client {
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
	// Thinking controls the DeepSeek/Kimi thinking mode toggle.
	// {"type": "enabled"} or {"type": "disabled"}. Omitted when nil.
	Thinking *thinkingToggle `json:"thinking,omitempty"`

	// UserID carries the DeepSeek user_id for isolation.
	// Omitted when empty.
	UserID string `json:"user_id,omitempty"`
	// ChatTemplateKwargs carries non-standard template parameters required
	// by certain transports. Used to enable thinking mode on Vertex AI's
	// deepseek-ai/deepseek-v3.2-maas, which silently ignores the standard
	// "thinking" field. See Capabilities.RequiresVertexThinkingKwargs.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	// routeResponses is the internal routing decision computed by
	// resolveAPIStrategy; consumed by resolveEndpoint. Never serialized.
	routeResponses bool `json:"-"`
}

type historyItem struct {
	Type      string  `json:"type"`
	Role      *string `json:"role,omitempty"`
	Content   []any   `json:"content,omitempty"` // requestContentBlock (text) or requestInputImageBlock (image); mixed []any
	CallID    *string `json:"call_id,omitempty"`
	Name      *string `json:"name,omitempty"`      // For function_call
	Arguments *string `json:"arguments,omitempty"` // For function_call
	Output    *string `json:"output,omitempty"`    // For function_call_output
}

type reasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

// thinkingToggle controls the DeepSeek thinking mode.
type thinkingToggle struct {
	Type string `json:"type"`
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

// videoURLBlock represents a video_url content part for video-capable models.
type videoURLBlock struct {
	Type     string        `json:"type"`
	VideoURL videoURLValue `json:"video_url"`
}

// videoURLValue holds the URL payload for a video_url block.
type videoURLValue struct {
	URL string `json:"url"`
}

// fileBlock references a previously uploaded file via the DeepSeek Files API.
type fileBlock struct {
	Type   string `json:"type"`
	FileID string `json:"file_id"`
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

// fileID returns the raw uploaded file ID for a part (no ms:// prefix).
func (ta *turnAssets) fileID(p *llm.Part) (string, bool) {
	if ta == nil {
		return "", false
	}
	id, ok := ta.bindings[p]
	return id, ok
}

// resolveURL returns the media URL value for a part: ms://{file_id}
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
	Type        string             `json:"type,omitempty"`
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

// shouldIncludeReasoning returns true when a reasoning_content field should
// be included on the outgoing message. It is included when the model supports
// reasoning_content natively and the role is assistant, or when non-empty
// reasoning text was produced by classifyParts.
func shouldIncludeReasoning(caps llm.Capabilities, role, reasoning string) bool {
	return (caps.SupportsReasoningContent && role == "assistant") || (reasoning != "")
}

// hasSupportedMedia returns true when the client supports vision or video
// and there are media parts to include in the message.
func hasSupportedMedia(caps llm.Capabilities, mediaParts []*llm.Part) bool {
	return (caps.SupportsVision || caps.SupportsVideo) && len(mediaParts) > 0
}

// buildMessageContent assembles the content value for a sink message. Returns
// a plain text string when no media parts are present or the model lacks
// vision/video support. Returns []any (mixed text + image_url/video_url blocks)
// otherwise, with media blocks placed before text (media-first ordering).
// Media parts dropped by capability filtering that would otherwise yield
// empty content are replaced by mediaOmittedFallback.
func (c *client) buildMessageContent(text string, mediaParts []*llm.Part, ta *turnAssets) any {
	if !hasSupportedMedia(c.capabilities, mediaParts) {
		if text != "" {
			return text
		}
		if len(mediaParts) > 0 {
			return c.mediaOmittedFallback(mediaParts)
		}
		return text
	}
	blocks := mediaBlocks(mediaParts, ta, c.capabilities)
	if text != "" {
		blocks = append(blocks, requestContentBlock{Type: "text", Text: text})
	}
	if len(blocks) == 0 {
		return c.mediaOmittedFallback(mediaParts)
	}
	return blocks
}

// mediaOmittedFallback returns a descriptive, truth-telling placeholder for
// user messages whose media parts were dropped because the active model does
// not support them. A non-empty string keeps the OpenAI wire contract valid
// (content must be a string or a list — never null or omitted) and preserves
// user-role message presence for strict role alternation.
func (c *client) mediaOmittedFallback(mediaParts []*llm.Part) string {
	for _, p := range mediaParts {
		if p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}
		mime := p.InlineData.MIMEType
		switch {
		case strings.HasPrefix(mime, "video/") && !c.capabilities.SupportsVideo:
			return "(video content omitted — this model does not support video input)"
		case strings.HasPrefix(mime, "image/") && !c.capabilities.SupportsVision:
			return "(image content omitted — this model does not support image input)"
		}
	}
	return "(media content omitted — this model does not support this media type)"
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
	if shouldIncludeReasoning(c.capabilities, role, reasoning) {
		reasoningPtr = &reasoning
	}

	content := c.buildMessageContent(text, mediaParts, ta)

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

// isMediaMIME returns true for MIME types that can be rendered as
// image_url or video_url content blocks in chat messages.
func isMediaMIME(mime string) bool {
	return strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/")
}

// extractMediaParts separates InlineData parts with image/* or video/*
// MIME types from a part slice. Returns the media parts and the
// remaining non-media, non-tool-response parts.
func extractMediaParts(parts []*llm.Part) (media []*llm.Part, rest []*llm.Part) {
	for _, p := range parts {
		if p.InlineData != nil && len(p.InlineData.Data) > 0 && isMediaMIME(p.InlineData.MIMEType) {
			media = append(media, p)
		} else {
			rest = append(rest, p)
		}
	}
	return
}

// isHydrationCandidate returns true when a part has an unresolved AssetID
// and its InlineData has not yet been populated. Parts without an AssetID
// or with already-present data are not candidates.
func isHydrationCandidate(p *llm.Part) bool {
	return p.AssetID != "" && (p.InlineData == nil || len(p.InlineData.Data) == 0)
}

// clonePartForHydration creates an independent copy of a part with hydrated
// InlineData. Preserves MIMEType from any existing InlineData blob (set
// during AddImage). Returns a pointer to the cloned part.
func clonePartForHydration(p *llm.Part, data []byte) *llm.Part {
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
	return &pc
}

// shouldSkipHydration returns true when hydration should be skipped entirely:
// no resolver is available or the model lacks both vision and video support.
func shouldSkipHydration(resolver llm.AssetResolver, caps llm.Capabilities) bool {
	return resolver == nil || (!caps.SupportsVision && !caps.SupportsVideo)
}

// hydrateMediaAssets resolves AssetID references on parts whose
// InlineData.Data is nil (lazy-hydration pattern from session reload).
// It uses copy-on-write — the returned slice is independent of the
// input — to avoid mutating shared session history. Preserves MIMEType
// from any existing InlineData blob. Gated on SupportsVision || SupportsVideo;
// non-vision/non-video models return the input unchanged. Resolve errors are
// propagated: a corrupt asset store should fail the session loudly.
func (c *client) hydrateMediaAssets(ctx context.Context, parts []*llm.Part, resolver llm.AssetResolver) ([]*llm.Part, error) {
	if shouldSkipHydration(resolver, c.capabilities) {
		return parts, nil
	}
	out := parts
	cloned := false
	for i, p := range parts {
		if !isHydrationCandidate(p) {
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
		out[i] = clonePartForHydration(p, data)
	}
	return out, nil
}

// fileExtensionFromMIME extracts the file extension from a MIME type
// string (e.g., "image/png" → "png"). Returns "png" as a safe default
// for empty or unparseable MIME types.
func fileExtensionFromMIME(mime string) string {
	if mime == "" {
		return "png"
	}
	if parts := strings.Split(mime, "/"); len(parts) == 2 {
		return parts[1]
	}
	return "png"
}

const (
	maxInlineMediaBytes = 32 << 20 // 32 MiB per inline image
	maxUploadMediaBytes = 64 << 20 // 64 MiB per Files API upload
	maxRequestBodyBytes = 48 << 20 // 48 MiB aggregate inline request body
)

// uploadMediaParts uploads media InlineData parts to the provider file API
// and records bindings in ta. Upload policy depends on the client's
// FileUploadMode: Kimi uploads all media parts; DeepSeek uploads only
// oversized image parts (> maxInlineMediaBytes); None uploads nothing.
// On upload failure, previously uploaded files in this batch are released
// and the error is returned.
func (c *client) uploadMediaParts(ctx context.Context, parts []*llm.Part, ta *turnAssets) error {
	if c.capabilities.FileUploadMode == llm.FileUploadNone {
		return nil
	}
	for _, p := range parts {
		purpose := c.mediaUploadPurpose(p)
		if purpose == "" {
			continue
		}
		if _, ok := ta.bindings[p]; ok {
			continue // already uploaded in this turn
		}
		ext := fileExtensionFromMIME(p.InlineData.MIMEType)
		filename := fmt.Sprintf("%s.%s", purpose, ext)
		fileID, err := c.uploadFile(ctx, p.InlineData.Data, filename, purpose)
		if err != nil {
			ta.release(ctx, c)
			return fmt.Errorf("upload media: %w", err)
		}
		ta.bindings[p] = fileID
		ta.uploaded = append(ta.uploaded, fileID)
	}
	return nil
}

// mediaUploadPurpose returns the file-API purpose for a part under the
// client's FileUploadMode, or "" when the part must not be uploaded.
func (c *client) mediaUploadPurpose(p *llm.Part) string {
	if p.InlineData == nil || len(p.InlineData.Data) == 0 {
		return ""
	}
	switch c.capabilities.FileUploadMode {
	case llm.FileUploadDeepSeek:
		if !strings.HasPrefix(p.InlineData.MIMEType, "image/") {
			return "" // images only; video is dropped for vision-only models
		}
		if len(p.InlineData.Data) <= maxInlineMediaBytes {
			return "" // small images stay inline (not uploaded)
		}
		return "user_data"
	case llm.FileUploadKimi:
		if !isMediaMIME(p.InlineData.MIMEType) {
			c.logger.Warn("skipping_unsupported_media_mime", "mime", p.InlineData.MIMEType)
			return ""
		}
		if strings.HasPrefix(p.InlineData.MIMEType, "video/") {
			return "video"
		}
		return "image"
	default:
		return ""
	}
}

// prepareMediaAssets hydrates and uploads media parts (image and video)
// for a single turn. For FileUploadDeepSeek, DeepSeek's size limits are
// enforced before any upload or inline serialization. Returns the
// turnAssets with file bindings and the prepared (possibly cloned) part
// slice. Caller must call release after the LLM response.
func (c *client) prepareMediaAssets(ctx context.Context, parts []*llm.Part, resolver llm.AssetResolver) (*turnAssets, []*llm.Part, error) {
	ta := newTurnAssets()

	out, err := c.hydrateMediaAssets(ctx, parts, resolver)
	if err != nil {
		return ta, out, err
	}

	if c.capabilities.FileUploadMode == llm.FileUploadDeepSeek {
		if err := c.checkDeepSeekMediaSizes(out); err != nil {
			return ta, out, err
		}
	}

	if c.capabilities.FileUploadMode != llm.FileUploadNone {
		if err := c.uploadMediaParts(ctx, out, ta); err != nil {
			return ta, out, err
		}
	}

	return ta, out, nil
}

// checkDeepSeekMediaSizes enforces the DeepSeek image size limits before
// any upload or inline serialization: single images over the upload cap
// fail the turn loudly, and the aggregate base64 size of images that stay
// inline must fit the request body cap. Video parts are ignored (vision-
// only models drop them).
func (c *client) checkDeepSeekMediaSizes(parts []*llm.Part) error {
	var inlineBase64Bytes int64
	for _, p := range parts {
		if p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}
		if !strings.HasPrefix(p.InlineData.MIMEType, "image/") {
			continue
		}
		n := int64(len(p.InlineData.Data))
		if n > maxUploadMediaBytes {
			return fmt.Errorf("image exceeds 64 MiB upload limit: %d bytes", n)
		}
		if n <= maxInlineMediaBytes {
			inlineBase64Bytes += ((n + 2) / 3) * 4
		}
	}
	if inlineBase64Bytes > maxRequestBodyBytes {
		return fmt.Errorf("aggregate inline image size exceeds 48 MiB limit")
	}
	return nil
}

// mediaBlocks converts InlineData parts to content blocks based on the
// model's capabilities. Image blocks are emitted only when
// caps.SupportsVision is true; video blocks only when caps.SupportsVideo
// is true (vision-only models drop video parts rather than emitting
// unsupported video_url blocks). File-upload modes control block shape:
// Kimi bound parts emit ms:// image_url/video_url blocks, DeepSeek bound
// parts emit file blocks, and unbound parts emit base64 data-URI blocks.
func mediaBlocks(parts []*llm.Part, ta *turnAssets, caps llm.Capabilities) []any {
	var blocks []any
	for _, p := range parts {
		if p.InlineData == nil || len(p.InlineData.Data) == 0 {
			continue
		}
		block, ok := mediaBlockFor(p, ta, caps)
		if !ok {
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// mediaBlockFor builds the content block for a single media part, or
// returns ok=false when the part must be dropped (unsupported MIME or
// unsupported media capability). Kept separate from mediaBlocks to keep
// cyclomatic complexity at or below the policy threshold (CC <= 10).
func mediaBlockFor(p *llm.Part, ta *turnAssets, caps llm.Capabilities) (any, bool) {
	mime := p.InlineData.MIMEType
	switch {
	case strings.HasPrefix(mime, "image/"):
		if !caps.SupportsVision {
			return nil, false
		}
		if caps.FileUploadMode == llm.FileUploadDeepSeek {
			if id, ok := ta.fileID(p); ok {
				return fileBlock{Type: "file", FileID: id}, true
			}
		}
		return imageURLBlock{Type: "image_url", ImageURL: imageURLValue{URL: ta.resolveURL(p)}}, true
	case strings.HasPrefix(mime, "video/"):
		if !caps.SupportsVideo {
			return nil, false
		}
		return videoURLBlock{Type: "video_url", VideoURL: videoURLValue{URL: ta.resolveURL(p)}}, true
	default:
		return nil, false
	}
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
	authHeaders := authcontract.AuthHeaders{}
	if err := c.authenticator.Apply(ctx, authHeaders); err != nil {
		return nil, err
	}
	for k, v := range authHeaders {
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
