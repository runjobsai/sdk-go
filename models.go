package runjobs

import (
	"context"
	"net/url"
)

// Model represents a model available on the RunJobs gateway.
type Model struct {
	ID                 string         `json:"id"`
	Object             string         `json:"object"`
	Capability         string         `json:"capability"`
	Provider           string         `json:"provider,omitempty"`
	Options            map[string]any `json:"options,omitempty"`
	InputPricePerMTok  int64          `json:"input_price_per_mtok"`
	OutputPricePerMTok int64          `json:"output_price_per_mtok"`
	FixedPrice         bool           `json:"fixed_price"`
	FixedCost          int64          `json:"fixed_cost"`
	MaxTokens          int            `json:"max_tokens"`
	MaxInputTokens     int            `json:"max_input_tokens"`
	IconURL            string         `json:"icon_url,omitempty"`
	AvailableVoices    []string       `json:"available_voices,omitempty"`
}

// ModelService provides access to the gateway's model endpoints.
type ModelService struct {
	client *Client
}

// ModelListOption configures a List call on ModelService.
type ModelListOption func(*modelListConfig)

type modelListConfig struct {
	capability string
}

// WithCapability filters the model list to models with the given capability.
func WithCapability(cap string) ModelListOption {
	return func(cfg *modelListConfig) { cfg.capability = cap }
}

// List returns all models available on the gateway, optionally filtered by capability.
func (s *ModelService) List(ctx context.Context, opts ...ModelListOption) ([]Model, error) {
	cfg := &modelListConfig{}
	for _, o := range opts {
		o(cfg)
	}

	path := "/v1/models"
	if cfg.capability != "" {
		path += "?" + url.Values{"capability": {cfg.capability}}.Encode()
	}

	var resp struct {
		Data []Model `json:"data"`
	}
	if err := s.client.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}
