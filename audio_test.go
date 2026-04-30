package runjobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAudioSpeech(t *testing.T) {
	audioBytes := []byte("fake-audio-mp3-data")
	b64Audio := base64.StdEncoding.EncodeToString(audioBytes)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"b64_audio": %q,
			"content_type": "audio/mpeg",
			"usage": {"total_cost": 0.015}
		}`, b64Audio)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Audio.Speech(context.Background(), "tts-1", SpeechParams{
		Input: "Hello world",
		Voice: "alloy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["model"] != "tts-1" {
		t.Fatalf("expected model tts-1, got %v", gotBody["model"])
	}
	if gotBody["input"] != "Hello world" {
		t.Fatalf("expected input 'Hello world', got %v", gotBody["input"])
	}
	// instruct_text omitempty: not sent when zero-valued.
	if _, has := gotBody["instruct_text"]; has {
		t.Fatalf("expected instruct_text omitted when empty, got %v", gotBody["instruct_text"])
	}

	if !bytes.Equal(resp.Data, audioBytes) {
		t.Fatalf("decoded audio mismatch: got %d bytes", len(resp.Data))
	}
	if resp.ContentType != "audio/mpeg" {
		t.Fatalf("expected content_type audio/mpeg, got %s", resp.ContentType)
	}
	if resp.Usage.TotalCost != 0.015 {
		t.Fatalf("expected total_cost 0.015, got %f", resp.Usage.TotalCost)
	}
}

func TestAudioListVoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/audio/voices" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if m := r.URL.Query().Get("model"); m != "MiniMax Speech 2.6 HD" {
			t.Fatalf("expected model query param, got %q", m)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"object": "list",
			"model": "MiniMax Speech 2.6 HD",
			"voices": [
				{"id": "English_CalmWoman", "name": "Calm Woman", "gender": "female", "language": "en"},
				{"id": "Japanese_KindLady", "name": "Kind Lady", "gender": "female", "language": "ja"}
			]
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	voices, err := c.Audio.ListVoices(context.Background(), "MiniMax Speech 2.6 HD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(voices.Voices) != 2 {
		t.Fatalf("expected 2 voices, got %d", len(voices.Voices))
	}
	if voices.Voices[0].ID != "English_CalmWoman" {
		t.Fatalf("expected first voice ID English_CalmWoman, got %s", voices.Voices[0].ID)
	}
	if voices.Voices[1].Language != "ja" {
		t.Fatalf("expected second voice language ja, got %s", voices.Voices[1].Language)
	}
}

func TestAudioTranscribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Fatalf("expected multipart, got %s", ct)
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("failed to parse multipart: %v", err)
		}
		if model := r.FormValue("model"); model != "whisper-1" {
			t.Fatalf("expected model whisper-1, got %s", model)
		}
		if lang := r.FormValue("language"); lang != "en" {
			t.Fatalf("expected language en, got %s", lang)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing file: %v", err)
		}
		defer file.Close()
		if header.Filename != "recording.mp3" {
			t.Fatalf("expected filename recording.mp3, got %s", header.Filename)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"text": "Hello world transcribed",
			"usage": {"total_cost": 0.006},
			"language": "en",
			"duration": 3.5
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Audio.Transcribe(context.Background(), "whisper-1", TranscribeParams{
		File:     bytes.NewReader([]byte("fake-audio")),
		Filename: "recording.mp3",
		Language: "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Hello world transcribed" {
		t.Fatalf("expected text 'Hello world transcribed', got %q", resp.Text)
	}
	if resp.Usage.TotalCost != 0.006 {
		t.Fatalf("expected total_cost 0.006, got %f", resp.Usage.TotalCost)
	}
	// Raw should contain extra fields (language, duration) but not text or usage
	if resp.Raw == nil {
		t.Fatal("expected Raw to be populated with extra fields")
	}
	if _, ok := resp.Raw["text"]; ok {
		t.Fatal("Raw should not contain 'text'")
	}
	if _, ok := resp.Raw["usage"]; ok {
		t.Fatal("Raw should not contain 'usage'")
	}
	if resp.Raw["language"] != "en" {
		t.Fatalf("expected Raw language 'en', got %v", resp.Raw["language"])
	}
}
