package runjobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestImageEdit_AsyncHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/images/edits", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST")
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Fatalf("expected multipart, got %s", ct)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if r.FormValue("prompt") != "add a hat" {
			t.Errorf("prompt = %q", r.FormValue("prompt"))
		}
		f, hdr, err := r.FormFile("image")
		if err != nil {
			t.Fatalf("missing image file: %v", err)
		}
		defer f.Close()
		if hdr.Filename != "photo.png" {
			t.Errorf("filename = %q", hdr.Filename)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"imgedit_abc","status":"queued"}`)
	})
	mux.HandleFunc("/v1/images/edits/imgedit_abc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"id":"imgedit_abc",
			"status":"succeeded",
			"data":[{"url":"%s/v1/blobs/imgout_edit.png"}],
			"usage":{"total_cost":0.02}
		}`, baseURLFromTestServer(r))
	})
	mux.HandleFunc("/v1/blobs/imgout_edit.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("EDITED"))
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
	resp, err := c.Image.Edit(context.Background(), "dall-e-2", ImageEditParams{
		Image:         bytes.NewReader([]byte("fake-image-data")),
		ImageFilename: "photo.png",
		Prompt:        "add a hat",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 image, got %d", len(resp.Data))
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("EDITED"))
	if resp.Data[0].B64JSON != wantB64 {
		t.Errorf("B64JSON = %q, want %q", resp.Data[0].B64JSON, wantB64)
	}
	if resp.Usage.TotalCost != 0.02 {
		t.Errorf("TotalCost = %v", resp.Usage.TotalCost)
	}
}

func TestImageGenerate_SurfacesFailedJobError(t *testing.T) {
	const detailedErr = "openai: 400 invalid_request_error: Your prompt was rejected by the content filter"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/images/generations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"img_fail","status":"queued"}`)
	})
	mux.HandleFunc("/v1/images/generations/img_fail", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"img_fail","status":"failed","error":%q}`, detailedErr)
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
	_, err := c.Image.Generate(context.Background(), "dall-e-3", ImageGenerateParams{Prompt: "forbidden"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 502 {
		t.Errorf("StatusCode = %d, want 502", apiErr.StatusCode)
	}
	if apiErr.Type != "image_job_failed" {
		t.Errorf("Type = %q, want image_job_failed", apiErr.Type)
	}
	if apiErr.Message != detailedErr {
		t.Errorf("Message = %q, want %q (this assertion is the whole point of the refactor)", apiErr.Message, detailedErr)
	}
}

func TestImageGenerate_CtxCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/images/generations", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"img_slow","status":"queued"}`)
	})
	mux.HandleFunc("/v1/images/generations/img_slow", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"img_slow","status":"running"}`)
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

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	_, err := c.Image.Generate(ctx, "dall-e-3", ImageGenerateParams{Prompt: "x"})
	if err == nil {
		t.Fatal("expected ctx deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}
