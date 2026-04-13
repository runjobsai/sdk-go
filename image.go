package runjobs

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
)

// ImageService provides access to the gateway's image endpoints.
type ImageService struct {
	client *Client
}

// ImageGenerateParams configures an image generation request.
type ImageGenerateParams struct {
	Prompt             string   `json:"prompt"`
	Size               string   `json:"size,omitempty"`
	N                  int      `json:"n,omitempty"`
	Quality            string   `json:"quality,omitempty"`
	Style              string   `json:"style,omitempty"`
	ResponseFormat     string   `json:"response_format,omitempty"`
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
	User               string   `json:"user,omitempty"`
}

// ImageResponse is the response from an image generation or edit request.
//
// Usage carries the gateway's billing total plus, when the upstream surfaces
// them, vendor-reported token counts (Seedream charges per output_token, so
// callers need OutputTokens for cost reconciliation).
type ImageResponse struct {
	Created int64         `json:"created"`
	Data    []ImageResult `json:"data"`
	Usage   ImageUsage    `json:"usage"`
}

// ImageResult is a single image in the response.
//
// Size is the actual pixel dimensions of THIS image as "WIDTHxHEIGHT".
// Adapters that return multi-image groups (Seedream sequential generation)
// may produce images of different sizes — read this per-result rather than
// assuming everything matches the request's `size` parameter.
type ImageResult struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	Size          string `json:"size,omitempty"`
}

// ImageUsage merges the gateway's billing fields with the upstream provider's
// token-usage envelope. Both groups arrive in the same JSON object — the
// gateway's TotalCost is always present; the token / tool fields are only
// populated for adapters whose API returns them (Ark Seedream, OpenAI gpt-image-1).
type ImageUsage struct {
	TotalCost       float64 `json:"total_cost"`
	GeneratedImages int     `json:"generated_images,omitempty"`
	OutputTokens    int     `json:"output_tokens,omitempty"`
	TotalTokens     int     `json:"total_tokens,omitempty"`
	// ToolUsage counts each tool the model invoked (e.g. Seedream
	// 5.0 lite web_search). Map key is the tool name.
	ToolUsage map[string]int `json:"tool_usage,omitempty"`
}

// ImageEditParams configures an image edit request.
type ImageEditParams struct {
	Image          io.Reader `json:"-"`
	ImageFilename  string    `json:"-"`
	Mask           io.Reader `json:"-"`
	MaskFilename   string    `json:"-"`
	Prompt         string    `json:"prompt"`
	Size           string    `json:"size,omitempty"`
	N              int       `json:"n,omitempty"`
	ResponseFormat string    `json:"response_format,omitempty"`
	User           string    `json:"user,omitempty"`
}

// Generate creates images from a text prompt. It submits the job, polls until
// completion, downloads the result blobs, and returns an *ImageResponse with
// B64JSON populated — identical to the old synchronous response shape.
func (s *ImageService) Generate(ctx context.Context, model string, params ImageGenerateParams) (*ImageResponse, error) {
	body := struct {
		Model string `json:"model"`
		ImageGenerateParams
	}{Model: model, ImageGenerateParams: params}

	var submit imageJobResponse
	if err := s.client.doJSON(ctx, "/v1/images/generations", body, &submit); err != nil {
		return nil, err
	}
	if submit.ID == "" {
		return nil, fmt.Errorf("runjobs: submit response missing job id")
	}
	return s.client.waitImageJob(ctx, "/v1/images/generations/"+submit.ID)
}

// Edit modifies an image given a prompt. The request is sent as multipart/form-data.
func (s *ImageService) Edit(ctx context.Context, model string, params ImageEditParams) (*ImageResponse, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		var err error
		defer func() {
			mw.Close()
			pw.CloseWithError(err)
		}()

		if err = mw.WriteField("model", model); err != nil {
			return
		}
		if err = mw.WriteField("prompt", params.Prompt); err != nil {
			return
		}
		if params.Size != "" {
			if err = mw.WriteField("size", params.Size); err != nil {
				return
			}
		}
		if params.N > 0 {
			if err = mw.WriteField("n", fmt.Sprintf("%d", params.N)); err != nil {
				return
			}
		}
		if params.ResponseFormat != "" {
			if err = mw.WriteField("response_format", params.ResponseFormat); err != nil {
				return
			}
		}
		if params.User != "" {
			if err = mw.WriteField("user", params.User); err != nil {
				return
			}
		}

		filename := params.ImageFilename
		if filename == "" {
			filename = "image.png"
		}
		var part io.Writer
		if part, err = mw.CreateFormFile("image", filename); err != nil {
			return
		}
		if _, err = io.Copy(part, params.Image); err != nil {
			return
		}

		if params.Mask != nil {
			maskFilename := params.MaskFilename
			if maskFilename == "" {
				maskFilename = "mask.png"
			}
			var maskPart io.Writer
			if maskPart, err = mw.CreateFormFile("mask", maskFilename); err != nil {
				return
			}
			if _, err = io.Copy(maskPart, params.Mask); err != nil {
				return
			}
		}
	}()

	var submit imageJobResponse
	if err := s.client.doMultipart(ctx, "/v1/images/edits", pr, mw.FormDataContentType(), &submit); err != nil {
		return nil, err
	}
	if submit.ID == "" {
		return nil, fmt.Errorf("runjobs: submit response missing job id")
	}
	return s.client.waitImageJob(ctx, "/v1/images/edits/"+submit.ID)
}
