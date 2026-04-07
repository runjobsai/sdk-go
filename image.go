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
	Prompt  string `json:"prompt"`
	Size    string `json:"size,omitempty"`
	N       int    `json:"n,omitempty"`
	Quality string `json:"quality,omitempty"`
	Style   string `json:"style,omitempty"`
}

// ImageResponse is the response from an image generation or edit request.
type ImageResponse struct {
	Created int64         `json:"created"`
	Data    []ImageResult `json:"data"`
	Usage   Usage         `json:"usage"`
}

// ImageResult is a single image in the response.
type ImageResult struct {
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageEditParams configures an image edit request.
type ImageEditParams struct {
	Image         io.Reader `json:"-"`
	ImageFilename string    `json:"-"`
	Mask          io.Reader `json:"-"`
	MaskFilename  string    `json:"-"`
	Prompt        string    `json:"prompt"`
	Size          string    `json:"size,omitempty"`
	N             int       `json:"n,omitempty"`
}

// Generate creates images from a text prompt.
func (s *ImageService) Generate(ctx context.Context, model string, params ImageGenerateParams) (*ImageResponse, error) {
	body := struct {
		Model string `json:"model"`
		ImageGenerateParams
	}{Model: model, ImageGenerateParams: params}

	var resp ImageResponse
	if err := s.client.doJSON(ctx, "/v1/images/generations", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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

	var resp ImageResponse
	if err := s.client.doMultipart(ctx, "/v1/images/edits", pr, mw.FormDataContentType(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
