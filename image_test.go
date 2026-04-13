package runjobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImageGenerate(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"created": 1700000000,
			"data": [{"b64_json": "aW1hZ2VkYXRh", "revised_prompt": "a cat sitting", "size": "1024x1024"}],
			"usage": {
				"total_cost": 0.04,
				"generated_images": 1,
				"output_tokens": 16384,
				"total_tokens": 16384,
				"tool_usage": {"web_search": 2}
			}
		}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	resp, err := c.Image.Generate(context.Background(), "dall-e-3", ImageGenerateParams{
		Prompt:  "a cat",
		Size:    "1024x1024",
		Quality: "hd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["model"] != "dall-e-3" {
		t.Fatalf("expected model dall-e-3, got %v", gotBody["model"])
	}
	if gotBody["prompt"] != "a cat" {
		t.Fatalf("expected prompt 'a cat', got %v", gotBody["prompt"])
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 image, got %d", len(resp.Data))
	}
	if resp.Data[0].B64JSON != "aW1hZ2VkYXRh" {
		t.Fatalf("unexpected b64_json: %s", resp.Data[0].B64JSON)
	}
	if resp.Data[0].RevisedPrompt != "a cat sitting" {
		t.Fatalf("unexpected revised prompt: %s", resp.Data[0].RevisedPrompt)
	}
	if resp.Data[0].Size != "1024x1024" {
		t.Fatalf("expected size 1024x1024, got %s", resp.Data[0].Size)
	}
	if resp.Usage.TotalCost != 0.04 {
		t.Fatalf("expected total_cost 0.04, got %f", resp.Usage.TotalCost)
	}
	if resp.Usage.GeneratedImages != 1 {
		t.Fatalf("expected generated_images 1, got %d", resp.Usage.GeneratedImages)
	}
	if resp.Usage.OutputTokens != 16384 {
		t.Fatalf("expected output_tokens 16384, got %d", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 16384 {
		t.Fatalf("expected total_tokens 16384, got %d", resp.Usage.TotalTokens)
	}
	if got := resp.Usage.ToolUsage["web_search"]; got != 2 {
		t.Fatalf("expected tool_usage.web_search 2, got %d", got)
	}
}

func TestImageEdit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Fatalf("expected multipart content type, got %s", ct)
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("failed to parse multipart: %v", err)
		}

		if model := r.FormValue("model"); model != "dall-e-2" {
			t.Fatalf("expected model dall-e-2, got %s", model)
		}
		if prompt := r.FormValue("prompt"); prompt != "add a hat" {
			t.Fatalf("expected prompt 'add a hat', got %s", prompt)
		}

		file, header, err := r.FormFile("image")
		if err != nil {
			t.Fatalf("missing image file: %v", err)
		}
		defer file.Close()
		if header.Filename != "photo.png" {
			t.Fatalf("expected filename photo.png, got %s", header.Filename)
		}
		imgData, _ := io.ReadAll(file)
		if string(imgData) != "fake-image-data" {
			t.Fatalf("unexpected image data: %s", string(imgData))
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"created": 1700000000,
			"data": [{"b64_json": "ZWRpdGVk"}],
			"usage": {"total_cost": 0.02}
		}`)
	}))
	defer srv.Close()

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
	if resp.Data[0].B64JSON != "ZWRpdGVk" {
		t.Fatalf("unexpected b64_json: %s", resp.Data[0].B64JSON)
	}
	if resp.Usage.TotalCost != 0.02 {
		t.Fatalf("expected total_cost 0.02, got %f", resp.Usage.TotalCost)
	}
}

func TestImageGenerateAsync_HappyPath(t *testing.T) {
	var pollCount int

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/async/images/generations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"img_test123","status":"queued"}`)
	})
	mux.HandleFunc("/v1/async/images/generations/img_test123", func(w http.ResponseWriter, r *http.Request) {
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
	resp, err := c.Image.GenerateAsync(context.Background(), "dall-e-3", ImageGenerateParams{Prompt: "a cat"})
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

func TestImageEditAsync_HappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/async/images/edits", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/async/images/edits/imgedit_abc", func(w http.ResponseWriter, r *http.Request) {
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
	resp, err := c.Image.EditAsync(context.Background(), "dall-e-2", ImageEditParams{
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

func TestImageGenerateAsync_SurfacesFailedJobError(t *testing.T) {
	const detailedErr = "openai: 400 invalid_request_error: Your prompt was rejected by the content filter"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/async/images/generations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"img_fail","status":"queued"}`)
	})
	mux.HandleFunc("/v1/async/images/generations/img_fail", func(w http.ResponseWriter, r *http.Request) {
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
	_, err := c.Image.GenerateAsync(context.Background(), "dall-e-3", ImageGenerateParams{Prompt: "forbidden"})
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

func TestImageGenerateAsync_CtxCancel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/async/images/generations", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"img_slow","status":"queued"}`)
	})
	mux.HandleFunc("/v1/async/images/generations/img_slow", func(w http.ResponseWriter, r *http.Request) {
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
	_, err := c.Image.GenerateAsync(ctx, "dall-e-3", ImageGenerateParams{Prompt: "x"})
	if err == nil {
		t.Fatal("expected ctx deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}
