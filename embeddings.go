package runjobs

import (
	"context"
	"encoding/json"
	"fmt"
)

// EmbeddingsService provides access to the gateway's vector-embedding
// endpoint. Backed by /v1/embeddings — same wire shape as OpenAI's
// SDK, so callers porting from `openai-go` can substitute the type
// names with minimal change.
type EmbeddingsService struct {
	client *Client
}

// EmbeddingsParams configures an embeddings request. Input is either
// a single string OR a slice of strings — Create's polymorphic Input
// field accepts either via type assertion at marshal time.
type EmbeddingsParams struct {
	// Input is either a string or []string. The gateway preserves the
	// shape on the wire — passing a single string returns
	// `data: [{embedding: [...]}]` (1-element array), passing a slice
	// returns one entry per input.
	Input any `json:"input"`
	// User is OpenAI's per-end-user observability tag.
	User string `json:"user,omitempty"`
	// EncodingFormat selects "float" (default) or "base64".
	EncodingFormat string `json:"encoding_format,omitempty"`
	// Dimensions truncates the output vector — text-embedding-3-* only.
	Dimensions int `json:"dimensions,omitempty"`
}

// Embedding is one entry in EmbeddingsResponse.Data. The vector is kept
// as json.RawMessage so callers using EncodingFormat="base64" don't pay
// the float-array unmarshal cost. Use AsFloat32() / AsFloat64() to
// materialise the vector when needed.
type Embedding struct {
	Object    string          `json:"object"` // always "embedding"
	Embedding json.RawMessage `json:"embedding"`
	Index     int             `json:"index"`
}

// AsFloat32 decodes the embedding into a float32 slice. Errors when
// EncodingFormat was "base64" — base64 payloads need a different
// decoder; use AsFloat64 (or write a base64-aware helper) instead.
func (e *Embedding) AsFloat32() ([]float32, error) {
	var out []float32
	if err := json.Unmarshal(e.Embedding, &out); err != nil {
		return nil, fmt.Errorf("runjobs: decode embedding (encoding_format=float?): %w", err)
	}
	return out, nil
}

// AsFloat64 decodes the embedding into a float64 slice. Slightly more
// memory than AsFloat32 but matches the gateway's JSON-native shape
// when EncodingFormat is left at the default "float".
func (e *Embedding) AsFloat64() ([]float64, error) {
	var out []float64
	if err := json.Unmarshal(e.Embedding, &out); err != nil {
		return nil, fmt.Errorf("runjobs: decode embedding (encoding_format=float?): %w", err)
	}
	return out, nil
}

// EmbeddingsUsage carries the gateway-stamped billing alongside the
// upstream-reported token counts. TotalCost is in USD (NOT pips).
type EmbeddingsUsage struct {
	PromptTokens int     `json:"prompt_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// EmbeddingsResponse mirrors OpenAI's response shape with the gateway's
// usage extensions.
type EmbeddingsResponse struct {
	Object string          `json:"object"` // always "list"
	Data   []Embedding     `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingsUsage `json:"usage"`
}

// Create issues a /v1/embeddings call and returns the assembled
// response. Wraps non-2xx upstreams as *APIError (caller can branch
// on .StatusCode / .Type / .Code).
func (s *EmbeddingsService) Create(ctx context.Context, model string, params EmbeddingsParams) (*EmbeddingsResponse, error) {
	body := struct {
		Model string `json:"model"`
		EmbeddingsParams
	}{Model: model, EmbeddingsParams: params}

	var resp EmbeddingsResponse
	if err := s.client.doJSON(ctx, "/v1/embeddings", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
