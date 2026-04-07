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

func TestComputerStep(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/computer/step" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"content": [
				{"type": "text", "text": "I see a desktop with icons."},
				{"type": "tool_use", "id": "tu-1", "name": "computer", "input": {"action": "click", "x": 100, "y": 200}}
			],
			"stop_reason": "tool_use",
			"usage": {
				"prompt_tokens": 500,
				"completion_tokens": 50,
				"total_cost": 0.008
			},
			"protocol": "anthropic"
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Computer.Step(context.Background(), "claude-sonnet", ComputerStepParams{
		Messages: []map[string]any{
			{"role": "user", "content": "Open the browser"},
		},
		DisplayWidth:  1920,
		DisplayHeight: 1080,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["model"] != "claude-sonnet" {
		t.Fatalf("expected model claude-sonnet, got %v", gotBody["model"])
	}

	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}

	// Check text block
	if resp.Content[0].Type != "text" {
		t.Fatalf("expected type text, got %s", resp.Content[0].Type)
	}
	if resp.Content[0].Text != "I see a desktop with icons." {
		t.Fatalf("unexpected text: %s", resp.Content[0].Text)
	}

	// Check tool_use block
	if resp.Content[1].Type != "tool_use" {
		t.Fatalf("expected type tool_use, got %s", resp.Content[1].Type)
	}
	if resp.Content[1].ID != "tu-1" {
		t.Fatalf("expected id tu-1, got %s", resp.Content[1].ID)
	}
	if resp.Content[1].Name != "computer" {
		t.Fatalf("expected name computer, got %s", resp.Content[1].Name)
	}
	// Input should be raw JSON
	var input map[string]any
	if err := json.Unmarshal(resp.Content[1].Input, &input); err != nil {
		t.Fatalf("failed to unmarshal input: %v", err)
	}
	if input["action"] != "click" {
		t.Fatalf("expected action click, got %v", input["action"])
	}

	if resp.StopReason != "tool_use" {
		t.Fatalf("expected stop_reason tool_use, got %s", resp.StopReason)
	}
	if resp.Usage.TotalCost != 0.008 {
		t.Fatalf("expected total_cost 0.008, got %f", resp.Usage.TotalCost)
	}
	if resp.Usage.PromptTokens != 500 {
		t.Fatalf("expected prompt_tokens 500, got %d", resp.Usage.PromptTokens)
	}
	if resp.Protocol != "anthropic" {
		t.Fatalf("expected protocol anthropic, got %s", resp.Protocol)
	}
}
