package runjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatNew(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("unexpected content type %s", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer gw-key" {
			t.Fatalf("unexpected auth header %s", auth)
		}

		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-1",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "gpt-4o",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello!"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15,
				"total_cost": 0.00123
			}
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Chat.New(context.Background(), ChatCompletionParams{
		Model: "gpt-4o",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stream was set to false in the request
	if stream, ok := gotBody["stream"].(bool); ok && stream {
		t.Fatal("expected stream=false in request body")
	}

	if resp.ID != "chatcmpl-1" {
		t.Fatalf("expected id chatcmpl-1, got %s", resp.ID)
	}
	if resp.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Fatalf("expected content Hello!, got %s", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalCost != 0.00123 {
		t.Fatalf("expected total_cost 0.00123, got %f", resp.Usage.TotalCost)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Fatalf("expected total_tokens 15, got %d", resp.Usage.TotalTokens)
	}
}

func TestChatNewError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error": "rate limited"}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	_, err := c.Chat.New(context.Background(), ChatCompletionParams{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 429 {
		t.Fatalf("expected status 429, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "rate limited" {
		t.Fatalf("expected message 'rate limited', got %q", apiErr.Message)
	}
	if apiErr.Type != "gateway_error" {
		t.Fatalf("expected type gateway_error, got %s", apiErr.Type)
	}
}

func TestChatNewStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(data, &body)
		if stream, ok := body["stream"].(bool); !ok || !stream {
			t.Fatalf("expected stream=true in request body")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"ch-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":""}]}`,
			`data: {"id":"ch-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"total_cost":0.005}}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			fmt.Fprintln(w, chunk)
			fmt.Fprintln(w) // blank line between events
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	stream := c.Chat.NewStreaming(context.Background(), ChatCompletionParams{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	defer stream.Close()

	var chunks []ChatCompletionChunk
	for stream.Next() {
		chunks = append(chunks, stream.Current())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Choices[0].Delta.Content != "Hel" {
		t.Fatalf("expected first chunk content 'Hel', got %q", chunks[0].Choices[0].Delta.Content)
	}
	if chunks[1].Choices[0].Delta.Content != "lo!" {
		t.Fatalf("expected second chunk content 'lo!', got %q", chunks[1].Choices[0].Delta.Content)
	}
	// Last chunk should have usage
	if chunks[1].Usage == nil {
		t.Fatal("expected usage in final chunk")
	}
	if chunks[1].Usage.TotalCost != 0.005 {
		t.Fatalf("expected total_cost 0.005, got %f", chunks[1].Usage.TotalCost)
	}
}

func TestChatNewStreamingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": "invalid model"}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	stream := c.Chat.NewStreaming(context.Background(), ChatCompletionParams{
		Model:    "nonexistent",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})
	defer stream.Close()

	if stream.Next() {
		t.Fatal("expected no chunks from errored stream")
	}
	err := stream.Err()
	if err == nil {
		t.Fatal("expected error from streaming")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Fatalf("expected status 400, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "invalid model" {
		t.Fatalf("expected message 'invalid model', got %q", apiErr.Message)
	}
}

func TestStreamClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"ch-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	stream := c.Chat.NewStreaming(context.Background(), ChatCompletionParams{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: "user", Content: "Hi"}},
	})

	// Close before consuming
	if err := stream.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}
