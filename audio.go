package runjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
)

// AudioService provides access to the gateway's audio endpoints.
type AudioService struct {
	client *Client
}

// SpeechParams holds parameters for text-to-speech generation.
// Emotion, Pitch, Volume, and Timber are supported by providers like MiniMax.
// InstructText is supported by self-hosted voiceclone providers (CosyVoice family).
//
// Zero-shot voice cloning: when ReferenceAudioURL is set, voiceclone-capable
// models ignore Voice and synthesize speech matching the timbre of the
// referenced clip.  ReferenceText (a transcript of what's said in the
// referenced audio) is optional but typically improves quality.  Nothing is
// stored server-side — every call re-fetches the URL.
type SpeechParams struct {
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
	// Emotion is provider-specific. Inspect the chosen TTS model on
	// the /v1/models response (Models service) — its
	// options.supported_emotions array enumerates the legal values.
	Emotion string `json:"emotion,omitempty"`
	Pitch          float64 `json:"pitch,omitempty"`         // -12 to 12 semitones
	Volume         float64 `json:"volume,omitempty"`        // 0.1 – 10.0 (1.0 = normal)
	Timber         float64 `json:"timber,omitempty"`        // -12 to 12 (voice timbre shift)
	InstructText   string  `json:"instruct_text,omitempty"` // free-form natural-language directive for voiceclone (CosyVoice). e.g. "用四川话快速地说"
	// ReferenceAudioURL points to a clip whose timbre the synth should
	// match (zero-shot voice cloning).  When set, voiceclone-family
	// providers ignore Voice and use this clip instead.  Provider must
	// be able to fetch the URL — i.e. it must be reachable from the
	// upstream service's network, not just the caller's.
	ReferenceAudioURL string `json:"reference_audio_url,omitempty"`
	// ReferenceText is the transcript of what's said in ReferenceAudioURL.
	// Optional — improves CosyVoice zero-shot quality when supplied.
	ReferenceText string `json:"reference_text,omitempty"`
	User          string `json:"user,omitempty"`

	// Extra carries vendor-specific top-level fields the canonical
	// SpeechParams doesn't model. Used for music-generation models on
	// the TTS bucket: ACE-Step needs `tags` (genre, required),
	// `duration`, `seed`, `scheduler`, `guidance_*` etc. The SDK
	// spreads Extra at the request body's TOP LEVEL (not nested) —
	// matches the gateway's extractSpeechExtra contract.
	Extra map[string]any `json:"-"`
}

// SpeechResponse holds the result of a text-to-speech request.
type SpeechResponse struct {
	Data        []byte `json:"-"`
	ContentType string `json:"-"`
	Usage       Usage  `json:"usage"`
}

// (Removed: Voice, VoiceCatalog, AudioService.ListVoices — voice metadata
// is now exposed entirely through /v1/models. Each text_to_speech model
// row carries `options.voices` and `options.supported_emotions`; pull the
// catalog via Client.Models.Get(ctx, modelName) and read those fields off
// the returned Model.Options map.)

// TranscribeParams holds parameters for audio transcription.
type TranscribeParams struct {
	File                    io.Reader `json:"-"`
	Filename                string    `json:"-"`
	Language                string    `json:"language,omitempty"`
	Prompt                  string    `json:"prompt,omitempty"`
	ResponseFormat          string    `json:"response_format,omitempty"`
	TimestampGranularities  []string  `json:"timestamp_granularities,omitempty"`
	User                    string    `json:"user,omitempty"`
}

// TranscribeResponse holds the result of an audio transcription request.
type TranscribeResponse struct {
	Text  string         `json:"text"`
	Usage Usage          `json:"usage"`
	Raw   map[string]any `json:"-"`
}

// Speech generates audio from text using the specified model.
func (s *AudioService) Speech(ctx context.Context, model string, params SpeechParams) (*SpeechResponse, error) {
	body, err := buildSpeechBody(model, params)
	if err != nil {
		return nil, err
	}
	return s.speechRaw(ctx, body)
}

// SpeechAsync is the async equivalent of Speech. It submits the job
// to the gateway's async TTS endpoint, polls until terminal, then
// decodes the result audio_url. Use this variant when a request is
// expected to run longer than ~100s — ACE-Step music generation at
// high settings, large CosyVoice batches, etc. The sync Speech
// method is the right choice for anything that fits inside
// Cloudflare's origin timeout (most short-clip TTS), since it skips
// the submit + poll round-trips.
//
// Returns the same *SpeechResponse shape as Speech — Data + Usage —
// so callers can swap the two methods with no other change.
func (s *AudioService) SpeechAsync(ctx context.Context, model string, params SpeechParams) (*SpeechResponse, error) {
	body, err := buildSpeechBody(model, params)
	if err != nil {
		return nil, err
	}
	var submit speechJobResponse
	if err := s.client.doJSON(ctx, "/v1/async/audio/speech", body, &submit); err != nil {
		return nil, err
	}
	if submit.ID == "" {
		return nil, fmt.Errorf("runjobs: speech submit response missing job id")
	}
	return s.client.waitSpeechJob(ctx, "/v1/async/audio/speech/"+submit.ID)
}

// buildSpeechBody marshals canonical SpeechParams plus a model name
// AND any Extra vendor fields, all at the request body's top level.
// The gateway's extractSpeechExtra peels non-canonical top-level
// fields back into req.Extra on the server side, so the round-trip
// is symmetric.
func buildSpeechBody(model string, params SpeechParams) (map[string]any, error) {
	canonical, err := json.Marshal(struct {
		Model string `json:"model"`
		SpeechParams
	}{Model: model, SpeechParams: params})
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(canonical, &body); err != nil {
		return nil, err
	}
	// Spread Extra at root. Canonical fields win — never let Extra
	// silently override a typed field.
	for k, v := range params.Extra {
		if _, claimed := body[k]; claimed {
			continue
		}
		body[k] = v
	}
	return body, nil
}

// SpeechRaw sends a pre-built JSON body to /v1/audio/speech.
// Use this when you need to forward a request body verbatim — e.g.
// proxying agent requests that may contain provider-specific fields
// outside SpeechParams.
func (s *AudioService) SpeechRaw(ctx context.Context, body json.RawMessage) (*SpeechResponse, error) {
	return s.speechRaw(ctx, body)
}

func (s *AudioService) speechRaw(ctx context.Context, body any) (*SpeechResponse, error) {
	// Wire shape: {audio_url: "data:<mime>;base64,...", usage: ...}.
	// The data: URI carries both the mime label and the base64 payload;
	// DecodeMediaURL handles both data: URIs and (for forward compat,
	// should the gateway ever switch to a hosted blob URL) http(s)
	// URLs symmetrically.
	var raw struct {
		AudioURL string `json:"audio_url"`
		Usage    Usage  `json:"usage"`
	}
	if err := s.client.doJSON(ctx, "/v1/audio/speech", body, &raw); err != nil {
		return nil, err
	}
	data, mime, err := DecodeMediaURL(ctx, raw.AudioURL)
	if err != nil {
		return nil, fmt.Errorf("runjobs: decode audio_url: %w", err)
	}
	return &SpeechResponse{
		Data:        data,
		ContentType: mime,
		Usage:       raw.Usage,
	}, nil
}

// Transcribe transcribes audio using the specified model.
func (s *AudioService) Transcribe(ctx context.Context, model string, params TranscribeParams) (*TranscribeResponse, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("model", model); err != nil {
		return nil, err
	}

	filename := params.Filename
	if filename == "" {
		filename = "audio.mp3"
	}
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, params.File); err != nil {
		return nil, err
	}

	if params.Language != "" {
		if err := w.WriteField("language", params.Language); err != nil {
			return nil, err
		}
	}
	if params.Prompt != "" {
		if err := w.WriteField("prompt", params.Prompt); err != nil {
			return nil, err
		}
	}
	if params.ResponseFormat != "" {
		if err := w.WriteField("response_format", params.ResponseFormat); err != nil {
			return nil, err
		}
	}
	for _, tg := range params.TimestampGranularities {
		if err := w.WriteField("timestamp_granularities[]", tg); err != nil {
			return nil, err
		}
	}
	if params.User != "" {
		if err := w.WriteField("user", params.User); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	var rawMsg json.RawMessage
	if err := s.client.doMultipart(ctx, "/v1/audio/transcriptions", &buf, w.FormDataContentType(), &rawMsg); err != nil {
		return nil, err
	}

	var resp TranscribeResponse
	if err := json.Unmarshal(rawMsg, &resp); err != nil {
		return nil, fmt.Errorf("runjobs: decode transcription: %w", err)
	}

	// Parse all fields into a map, then remove text and usage to populate Raw.
	var all map[string]any
	if err := json.Unmarshal(rawMsg, &all); err != nil {
		return nil, fmt.Errorf("runjobs: decode transcription raw: %w", err)
	}
	delete(all, "text")
	delete(all, "usage")
	if len(all) > 0 {
		resp.Raw = all
	}

	return &resp, nil
}
