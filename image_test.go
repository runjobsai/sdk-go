package runjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
