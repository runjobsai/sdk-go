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
type ChatMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
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
type ChatChoiceMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
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
