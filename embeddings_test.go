package runjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEmbeddingsCreate covers the wire-format roundtrip the way callers
// actually use it: pass a slice of strings, get back per-input
// embeddings + usage info. Includes the AsFloat32 helper since most
// real consumers go straight from API response → []float32 for the
// vector store.
func TestEmbeddingsCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["model"] != "text-embedding-3-small" {
			t.Fatalf("model = %v", got["model"])
		}
		input, _ := got["input"].([]any)
		if len(input) != 2 || input[0] != "alpha" || input[1] != "beta" {
			t.Fatalf("input = %v", got["input"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"object": "list",
			"data": [
				{"object": "embedding", "embedding": [0.1, 0.2, 0.3], "index": 0},
				{"object": "embedding", "embedding": [0.4, 0.5, 0.6], "index": 1}
			],
			"model": "text-embedding-3-small",
			"usage": {"prompt_tokens": 6, "total_tokens": 6, "total_cost": 0.000012}
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Embeddings.Create(context.Background(), "text-embedding-3-small", EmbeddingsParams{
		Input: []string{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("object = %q", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Data))
	}
	if resp.Data[0].Index != 0 || resp.Data[1].Index != 1 {
		t.Errorf("indexes wrong: %d %d", resp.Data[0].Index, resp.Data[1].Index)
	}
	v0, err := resp.Data[0].AsFloat32()
	if err != nil {
		t.Fatalf("AsFloat32: %v", err)
	}
	if len(v0) != 3 || v0[0] < 0.099 || v0[0] > 0.101 {
		t.Errorf("vec[0] = %v, want ~[0.1,0.2,0.3]", v0)
	}
	if resp.Usage.PromptTokens != 6 {
		t.Errorf("prompt_tokens = %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.TotalCost <= 0 {
		t.Errorf("total_cost should be > 0, got %v", resp.Usage.TotalCost)
	}
}

// TestEmbeddingsCreate_ScalarInput verifies the single-string form
// rounds the wire — many docs / examples use the scalar shape, the
// SDK can't accidentally array-wrap it.
func TestEmbeddingsCreate_ScalarInput(t *testing.T) {
	var gotInput json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]json.RawMessage
		_ = json.Unmarshal(body, &got)
		gotInput = got["input"]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3,"total_cost":0.0}}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	_, err := c.Embeddings.Create(context.Background(), "text-embedding-3-small", EmbeddingsParams{
		Input: "a single sentence",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(gotInput) != `"a single sentence"` {
		t.Errorf("input on wire = %s, want quoted string literal", gotInput)
	}
}
