package runjobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ChatService provides access to the gateway's chat completion endpoints.
type ChatService struct {
	client *Client
}

// ChatMessage represents a single message in the conversation.
// Content can be a string for simple text messages, or a []ContentPart
// for multi-modal messages (e.g. text + image).
type ChatMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  []ChatToolCall `json:"tool_calls,omitempty"`
}

// ContentPart is a single part of a multi-modal message content array.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL references an image by URL or base64 data URI.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto" | "low" | "high"
}

// UserMessage creates a simple text user message.
func UserMessage(content string) ChatMessage {
	return ChatMessage{Role: "user", Content: content}
}

// SystemMessage creates a system message.
func SystemMessage(content string) ChatMessage {
	return ChatMessage{Role: "system", Content: content}
}

// AssistantMessage creates an assistant message.
func AssistantMessage(content string) ChatMessage {
	return ChatMessage{Role: "assistant", Content: content}
}

// ToolResultMessage creates a tool result message.
func ToolResultMessage(toolCallID, content string) ChatMessage {
	return ChatMessage{Role: "tool", ToolCallID: toolCallID, Content: content}
}

// UserMessageParts creates a multi-modal user message from content parts.
func UserMessageParts(parts ...ContentPart) ChatMessage {
	return ChatMessage{Role: "user", Content: parts}
}

// TextPart creates a text content part.
func TextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

// ImagePart creates an image_url content part.
func ImagePart(url string, detail ...string) ContentPart {
	p := ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: url}}
	if len(detail) > 0 {
		p.ImageURL.Detail = detail[0]
	}
	return p
}

// ChatCompletionParams configures a chat completion request.
type ChatCompletionParams struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Stop             []string      `json:"stop,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	N                *int          `json:"n,omitempty"`
	Tools            []ChatTool    `json:"tools,omitempty"`
	ToolChoice       any           `json:"tool_choice,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
}

// ChatTool describes a tool available to the model.
type ChatTool struct {
	Type     string       `json:"type"`
	Function ChatFunction `json:"function"`
}

// ChatFunction describes a function tool.
type ChatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ChatCompletion is the response from a non-streaming chat completion request.
type ChatCompletion struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatChoice       `json:"choices"`
	Usage   ChatCompletionUsage `json:"usage"`
}

// ChatChoice is a single choice in the chat completion response.
type ChatChoice struct {
	Index        int                `json:"index"`
	Message      ChatChoiceMessage  `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

// ChatChoiceMessage is the assistant message in a choice.
// Content is typically a string, but may be null when the model only
// produces tool calls.
type ChatChoiceMessage struct {
	Role      string         `json:"role"`
	Content   any            `json:"content"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

// ContentString returns the content as a string. Returns "" if content
// is nil or not a string (e.g. when the model only produced tool calls).
func (m ChatChoiceMessage) ContentString() string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	return ""
}

// ChatToolCall represents a tool call made by the model.
type ChatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function ChatToolCallFunction `json:"function"`
}

// ChatToolCallFunction contains the function name and arguments.
type ChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionUsage reports token consumption and cost.
type ChatCompletionUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCost        float64 `json:"total_cost"`
}

// ChatCompletionChunk is a single chunk in a streaming chat completion response.
type ChatCompletionChunk struct {
	ID      string                  `json:"id"`
	Object  string                  `json:"object"`
	Created int64                   `json:"created"`
	Model   string                  `json:"model"`
	Choices []ChatChunkChoice       `json:"choices"`
	Usage   *ChatCompletionUsage    `json:"usage,omitempty"`
}

// ChatChunkChoice is a single choice in a streaming chunk.
type ChatChunkChoice struct {
	Index        int              `json:"index"`
	Delta        ChatChunkDelta   `json:"delta"`
	FinishReason string           `json:"finish_reason,omitempty"`
}

// ChatChunkDelta contains the incremental content in a streaming chunk.
type ChatChunkDelta struct {
	Role      string         `json:"role,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

// New creates a chat completion (non-streaming).
func (s *ChatService) New(ctx context.Context, params ChatCompletionParams) (*ChatCompletion, error) {
	params.Stream = false
	var resp ChatCompletion
	if err := s.client.doJSON(ctx, "/v1/chat/completions", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Stream is an iterator over streaming chat completion chunks.
type Stream struct {
	scanner *bufio.Scanner
	resp    *http.Response
	current ChatCompletionChunk
	err     error
	done    bool
}

// Next advances to the next chunk. Returns false when the stream is exhausted or an error occurs.
func (s *Stream) Next() bool {
	if s.done || s.err != nil {
		return false
	}
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			s.done = true
			return false
		}
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			s.err = fmt.Errorf("runjobs: decode stream chunk: %w", err)
			return false
		}
		s.current = chunk
		return true
	}
	if err := s.scanner.Err(); err != nil {
		s.err = fmt.Errorf("runjobs: read stream: %w", err)
	}
	s.done = true
	return false
}

// Current returns the most recent chunk read by Next.
func (s *Stream) Current() ChatCompletionChunk {
	return s.current
}

// Err returns any error encountered during streaming.
func (s *Stream) Err() error {
	return s.err
}

// Close releases the underlying HTTP response body.
func (s *Stream) Close() error {
	if s.resp != nil {
		return s.resp.Body.Close()
	}
	return nil
}

// NewRaw sends a pre-built JSON body to /v1/chat/completions (non-streaming).
// Use this when you need to forward a request body verbatim — e.g. proxying
// agent requests that may contain fields outside ChatCompletionParams.
func (s *ChatService) NewRaw(ctx context.Context, body json.RawMessage) (*ChatCompletion, error) {
	var resp ChatCompletion
	if err := s.client.doJSON(ctx, "/v1/chat/completions", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NewStreamingRaw sends a pre-built JSON body to /v1/chat/completions with
// streaming enabled, returning a Stream iterator. The caller should ensure
// the body includes "stream":true and "stream_options":{"include_usage":true}.
func (s *ChatService) NewStreamingRaw(ctx context.Context, body json.RawMessage) *Stream {
	stream := &Stream{}

	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		stream.err = err
		stream.done = true
		return stream
	}

	// Disable gzip so SSE lines arrive immediately.
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		stream.err = err
		stream.done = true
		return stream
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		stream.err = s.client.readError(req, resp)
		stream.done = true
		return stream
	}

	stream.resp = resp
	stream.scanner = bufio.NewScanner(resp.Body)
	return stream
}

// NewStreaming creates a streaming chat completion, returning a Stream iterator.
func (s *ChatService) NewStreaming(ctx context.Context, params ChatCompletionParams) *Stream {
	params.Stream = true

	stream := &Stream{}

	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/v1/chat/completions", params)
	if err != nil {
		stream.err = err
		stream.done = true
		return stream
	}

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		stream.err = err
		stream.done = true
		return stream
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		stream.err = s.client.readError(req, resp)
		stream.done = true
		return stream
	}

	stream.resp = resp
	stream.scanner = bufio.NewScanner(resp.Body)
	return stream
}
