package runjobs

import (
	"bytes"
	"context"
	"encoding/base64"
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
type SpeechParams struct {
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
	User           string  `json:"user,omitempty"`
}

// SpeechResponse holds the result of a text-to-speech request.
type SpeechResponse struct {
	Data        []byte `json:"-"`
	ContentType string `json:"-"`
	Usage       Usage  `json:"usage"`
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
