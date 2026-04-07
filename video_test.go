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
