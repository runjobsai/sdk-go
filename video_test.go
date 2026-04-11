package runjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestVideoGenerate(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/videos/generations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "vtask-123",
			"status": "processing",
			"usage": {"total_cost": 0.10}
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	task, err := c.Video.Generate(context.Background(), "sora", VideoGenerateParams{
		Prompt:      "a flying cat",
		AspectRatio: "16:9",
		Duration:    5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["model"] != "sora" {
		t.Fatalf("expected model sora, got %v", gotBody["model"])
	}
	if gotBody["prompt"] != "a flying cat" {
		t.Fatalf("expected prompt 'a flying cat', got %v", gotBody["prompt"])
	}

	if task.ID != "vtask-123" {
		t.Fatalf("expected id vtask-123, got %s", task.ID)
	}
	if task.Status != "processing" {
		t.Fatalf("expected status processing, got %s", task.Status)
	}
	if task.Usage.TotalCost != 0.10 {
		t.Fatalf("expected total_cost 0.10, got %f", task.Usage.TotalCost)
	}
}

func TestVideoGetStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/videos/generations/vtask-123" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "vtask-123",
			"status": "processing",
			"progress": 50
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	status, err := c.Video.GetStatus(context.Background(), "vtask-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.ID != "vtask-123" {
		t.Fatalf("expected id vtask-123, got %s", status.ID)
	}
	if status.Status != "processing" {
		t.Fatalf("expected status processing, got %s", status.Status)
	}
	if status.Progress != 50 {
		t.Fatalf("expected progress 50, got %d", status.Progress)
	}
}

// TestVideoStatusSucceededFields covers the optional metadata block the
// gateway exposes only on terminal succeeded states (Seedance 2.0:
// last_frame_url, duration, fps, resolution, ratio, seed, usage_tokens).
func TestVideoStatusSucceededFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "vtask-789",
			"status": "succeeded",
			"video_url": "https://example.com/v.mp4",
			"last_frame_url": "https://example.com/v_last.png",
			"duration": 5,
			"fps": 24,
			"resolution": "720p",
			"ratio": "16:9",
			"seed": 12345,
			"service_tier": "default",
			"usage_tokens": {"completion_tokens": 108900, "total_tokens": 108900},
			"created_at": 1700000000,
			"updated_at": 1700000300
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	status, err := c.Video.GetStatus(context.Background(), "vtask-789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.LastFrameURL != "https://example.com/v_last.png" {
		t.Fatalf("missing last_frame_url: %q", status.LastFrameURL)
	}
	if status.Duration != 5 || status.FPS != 24 {
		t.Fatalf("duration/fps mismatch: %d/%d", status.Duration, status.FPS)
	}
	if status.Resolution != "720p" || status.Ratio != "16:9" {
		t.Fatalf("resolution/ratio mismatch: %s/%s", status.Resolution, status.Ratio)
	}
	if status.Seed != 12345 {
		t.Fatalf("seed mismatch: %d", status.Seed)
	}
	if status.ServiceTier != "default" {
		t.Fatalf("service_tier mismatch: %s", status.ServiceTier)
	}
	if status.UsageTokens == nil || status.UsageTokens.CompletionTokens != 108900 {
		t.Fatalf("usage_tokens missing or wrong: %+v", status.UsageTokens)
	}
	if status.CreatedAt == 0 || status.UpdatedAt == 0 {
		t.Fatalf("created_at/updated_at missing: %d/%d", status.CreatedAt, status.UpdatedAt)
	}
}

// TestVideoGenerateNewParams asserts every new top-level Seedance 2.0 field
// the gateway accepts is serialised verbatim into the outgoing JSON body.
func TestVideoGenerateNewParams(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": "vtask-1", "status": "queued", "usage": {"total_cost": 0}}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	wm := false
	cf := true
	rlf := true
	_, err := c.Video.Generate(context.Background(), "doubao-seedance-2-0-260128", VideoGenerateParams{
		Prompt:                "a cat",
		AspectRatio:           "16:9",
		Duration:              5,
		Resolution:            "720p",
		ReferenceImageURLs:    []string{"https://example.com/r1.jpg", "https://example.com/r2.jpg"},
		ReferenceVideoURLs:    []string{"https://example.com/rv1.mp4"},
		ReferenceAudioURLs:    []string{"https://example.com/ra1.mp3"},
		Watermark:             &wm,
		CameraFixed:           &cf,
		ReturnLastFrame:       &rlf,
		Seed:                  12345,
		ServiceTier:           "flex",
		ExecutionExpiresAfter: 172800,
		CallbackURL:           "https://example.com/hook",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := map[string]any{
		"seed":                    float64(12345),
		"watermark":               false,
		"camera_fixed":            true,
		"return_last_frame":       true,
		"service_tier":            "flex",
		"execution_expires_after": float64(172800),
		"callback_url":            "https://example.com/hook",
	}
	for k, want := range checks {
		if gotBody[k] != want {
			t.Errorf("body[%q] = %v, want %v", k, gotBody[k], want)
		}
	}
	if refs, ok := gotBody["reference_image_urls"].([]any); !ok || len(refs) != 2 {
		t.Errorf("reference_image_urls not serialised: %v", gotBody["reference_image_urls"])
	}
	if vids, ok := gotBody["reference_video_urls"].([]any); !ok || len(vids) != 1 {
		t.Errorf("reference_video_urls not serialised: %v", gotBody["reference_video_urls"])
	}
	if auds, ok := gotBody["reference_audio_urls"].([]any); !ok || len(auds) != 1 {
		t.Errorf("reference_audio_urls not serialised: %v", gotBody["reference_audio_urls"])
	}
}

func TestVideoWait(t *testing.T) {
	var pollCount int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&pollCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			fmt.Fprintf(w, `{"id": "vtask-123", "status": "processing", "progress": %d}`, n*30)
		} else {
			fmt.Fprint(w, `{"id": "vtask-123", "status": "succeeded", "progress": 100, "video_url": "https://example.com/video.mp4"}`)
		}
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	status, err := c.Video.Wait(context.Background(), "vtask-123", WithPollInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "succeeded" {
		t.Fatalf("expected status succeeded, got %s", status.Status)
	}
	if status.VideoURL != "https://example.com/video.mp4" {
		t.Fatalf("expected video_url, got %s", status.VideoURL)
	}
	if atomic.LoadInt64(&pollCount) != 3 {
		t.Fatalf("expected 3 polls, got %d", atomic.LoadInt64(&pollCount))
	}
}

func TestVideoWaitFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": "vtask-456", "status": "failed", "error": "content policy"}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	status, err := c.Video.Wait(context.Background(), "vtask-456", WithPollInterval(10*time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != "failed" {
		t.Fatalf("expected status failed, got %s", status.Status)
	}
	if status.Error != "content policy" {
		t.Fatalf("expected error 'content policy', got %q", status.Error)
	}
}

func TestVideoGetContent(t *testing.T) {
	videoData := []byte("fake-mp4-video-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/videos/vtask-123/content" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Write(videoData)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	data, mime, err := c.Video.GetContent(context.Background(), "vtask-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "fake-mp4-video-bytes" {
		t.Fatalf("unexpected data: %s", string(data))
	}
	if mime != "video/mp4" {
		t.Fatalf("expected video/mp4, got %s", mime)
	}
}
