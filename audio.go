package runjobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
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
	Emotion        string  `json:"emotion,omitempty"`       // optional; use ListVoices().SupportedEmotions for valid values
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
}

// SpeechResponse holds the result of a text-to-speech request.
type SpeechResponse struct {
	Data        []byte `json:"-"`
	ContentType string `json:"-"`
	Usage       Usage  `json:"usage"`
}

// Voice describes a single voice available for text-to-speech.
type Voice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Gender   string `json:"gender,omitempty"`
	Language string `json:"language,omitempty"`
}

// VoiceCatalog holds the full response from ListVoices, including voices
// and optional model capabilities like supported emotions.
type VoiceCatalog struct {
	Voices            []Voice  `json:"voices"`
	SupportedEmotions []string `json:"supported_emotions,omitempty"`
}

// ListVoices returns the available voices and capabilities for the given TTS model.
func (s *AudioService) ListVoices(ctx context.Context, model string) (*VoiceCatalog, error) {
	path := "/v1/audio/voices?model=" + url.QueryEscape(model)
	var resp VoiceCatalog
	if err := s.client.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

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
	body := struct {
		Model string `json:"model"`
		SpeechParams
	}{Model: model, SpeechParams: params}

	var raw struct {
		B64Audio    string `json:"b64_audio"`
		ContentType string `json:"content_type"`
		Usage       Usage  `json:"usage"`
	}
	if err := s.client.doJSON(ctx, "/v1/audio/speech", body, &raw); err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(raw.B64Audio)
	if err != nil {
		return nil, fmt.Errorf("runjobs: decode b64_audio: %w", err)
	}

	return &SpeechResponse{
		Data:        data,
		ContentType: raw.ContentType,
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
