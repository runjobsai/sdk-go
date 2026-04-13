package runjobs

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestImageGenerate_AsyncHappyPath(t *testing.T) {
	var pollCount int

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/images/generations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"img_test123","status":"queued"}`)
	})
	mux.HandleFunc("/v1/images/generations/img_test123", func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		w.Header().Set("Content-Type", "application/json")
		if pollCount < 2 {
			fmt.Fprint(w, `{"id":"img_test123","status":"running"}`)
			return
		}
		fmt.Fprintf(w, `{
			"id":"img_test123",
			"status":"succeeded",
			"data":[{"url":"%s/v1/blobs/imgout_fake.png","size":"1024x1024","revised_prompt":"a cat"}],
			"usage":{"total_cost":0.04,"generated_images":1,"total_tokens":128}
		}`, baseURLFromTestServer(r))
	})
	mux.HandleFunc("/v1/blobs/imgout_fake.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("PNGDATA"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldFirstDelay, oldPollInterval := imagePollFirstDelay, imagePollInterval
	imagePollFirstDelay = 10 * time.Millisecond
	imagePollInterval = 10 * time.Millisecond
	defer func() {
		imagePollFirstDelay = oldFirstDelay
		imagePollInterval = oldPollInterval
	}()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Image.Generate(context.Background(), "dall-e-3", ImageGenerateParams{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 image, got %d", len(resp.Data))
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("PNGDATA"))
	if resp.Data[0].B64JSON != wantB64 {
		t.Errorf("B64JSON = %q, want %q", resp.Data[0].B64JSON, wantB64)
	}
	if resp.Data[0].Size != "1024x1024" {
		t.Errorf("Size = %q", resp.Data[0].Size)
	}
	if resp.Data[0].RevisedPrompt != "a cat" {
		t.Errorf("RevisedPrompt = %q", resp.Data[0].RevisedPrompt)
	}
	if resp.Usage.TotalCost != 0.04 {
		t.Errorf("TotalCost = %v", resp.Usage.TotalCost)
	}
	if resp.Usage.GeneratedImages != 1 {
		t.Errorf("GeneratedImages = %d", resp.Usage.GeneratedImages)
	}
}

func baseURLFromTestServer(r *http.Request) string {
	return "http://" + r.Host
}
