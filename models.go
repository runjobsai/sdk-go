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

// optBool reads a boolean flag from m.Options. Treats both bool true and
// numeric 1 as true (admin UI may serialise either way).
func (m Model) optBool(key string) bool {
	if v, ok := m.Options[key].(bool); ok {
		return v
	}
	if v, ok := m.Options[key].(float64); ok {
		return v != 0
	}
	return false
}

// SupportsVoiceClone reports whether this TTS model accepts a reference
// audio sample as the `voice` parameter on /v1/audio/speech. When true,
// the `voice` field in SpeechParams may be:
//   - an https:// URL to a wav file (≤30s of clean speech recommended), or
//   - a data:audio/wav;base64,… URI (inline upload, ≤30s)
//
// Models without this flag only accept catalog voice IDs returned by
// ListVoices. Set on text_to_speech models only; ignored on others.
func (m Model) SupportsVoiceClone() bool { return m.optBool("supports_voice_clone") }

// SupportsInstructText reports whether this TTS model accepts a free-form
// natural-language style directive (SpeechParams.InstructText). Used by
// CosyVoice-family voiceclone providers — the directive overrides any
// emotion/speed/volume hints.
func (m Model) SupportsInstructText() bool { return m.optBool("supports_instruct_text") }

// DefaultVoice returns the model's configured default voice (admin-set
// via Options.default_voice), or "" if unset. Useful for picking a voice
// when the caller doesn't have a preference.
func (m Model) DefaultVoice() string {
	if v, ok := m.Options["default_voice"].(string); ok {
		return v
	}
	return ""
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
