package runjobs

import (
	"context"
	"encoding/json"
)

// ComputerService provides access to the gateway's computer use endpoint.
type ComputerService struct {
	client *Client
}

// ComputerStepParams are the parameters for a single computer use step.
type ComputerStepParams struct {
	// Messages is the conversation history including screenshots and tool results.
	// Each entry is a map with role + content (content may be a string or array
	// of blocks). The format is intentionally opaque to support both Anthropic
	// and OpenAI computer-use protocols.
	Messages []map[string]any `json:"messages"`

	// DisplayWidth and DisplayHeight describe the screen resolution.
	// Defaults to 1920x1080 if omitted.
	DisplayWidth  int  `json:"display_width,omitempty"`
	DisplayHeight int  `json:"display_height,omitempty"`
	MaxTokens     int  `json:"max_tokens,omitempty"`
	EnableZoom    bool `json:"enable_zoom,omitempty"`

	// PreviousResponseID is for OpenAI Responses-API state chaining.
	PreviousResponseID string `json:"previous_response_id,omitempty"`

	// OpenAIInput is a follow-up computer_call_output for OpenAI's Responses API.
	OpenAIInput any    `json:"openai_input,omitempty"`
	User        string `json:"user,omitempty"`
}

// ComputerResponse contains the model's action plan for this step.
type ComputerResponse struct {
	Content    []ComputerContentBlock `json:"content"`
	StopReason string                 `json:"stop_reason,omitempty"`
	Usage      ComputerUsage          `json:"usage"`
	ResponseID string                 `json:"response_id,omitempty"`
	Protocol   string                 `json:"protocol,omitempty"`
}

// ComputerContentBlock is a single action or text block in the response.
// Inspect Type to dispatch: "text", "tool_use" (Anthropic), "computer_call" (OpenAI).
type ComputerContentBlock struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	ID     string          `json:"id,omitempty"`
	Name   string          `json:"name,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	CallID string          `json:"call_id,omitempty"`
	Action map[string]any  `json:"action,omitempty"`
}

// ComputerUsage is an alias for Usage.
type ComputerUsage = Usage

// Step executes one step of a computer use agent loop: given a screenshot
// and conversation history, returns the next action(s) the model wants executed.
func (s *ComputerService) Step(ctx context.Context, model string, params ComputerStepParams) (*ComputerResponse, error) {
	body := struct {
		Model string `json:"model"`
		ComputerStepParams
	}{Model: model, ComputerStepParams: params}

	var resp ComputerResponse
	if err := s.client.doJSON(ctx, "/v1/computer/step", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
