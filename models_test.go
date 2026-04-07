package runjobs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer gw-key" {
			t.Fatalf("unexpected auth %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [
				{"id": "gpt-4o", "object": "model", "capability": "chat", "max_tokens": 4096},
				{"id": "dall-e-3", "object": "model", "capability": "image", "max_tokens": 0}
			]
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	models, err := c.Models.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", models[0].ID)
	}
	if models[0].Capability != "chat" {
		t.Fatalf("expected capability chat, got %s", models[0].Capability)
	}
	if models[1].ID != "dall-e-3" {
		t.Fatalf("expected dall-e-3, got %s", models[1].ID)
	}
}

func TestModelsListWithCapability(t *testing.T) {
	var gotCapability string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCapability = r.URL.Query().Get("capability")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data": [{"id": "gpt-4o", "object": "model", "capability": "chat"}]}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	models, err := c.Models.List(context.Background(), WithCapability("chat"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCapability != "chat" {
		t.Fatalf("expected capability query param 'chat', got %q", gotCapability)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
}
