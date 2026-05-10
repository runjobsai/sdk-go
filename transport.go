package runjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// newJSONRequest builds an authenticated JSON POST request.
func (c *Client) newJSONRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("runjobs: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return req, nil
}

// readError reads the response body and constructs an *APIError.
func (c *Client) readError(_ *http.Request, resp *http.Response) error {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("runjobs: read error body: %w", err)
	}
	return c.makeError(resp, data)
}

// doJSON sends a JSON POST request to the given path, decoding the response into dst.
func (c *Client) doJSON(ctx context.Context, path string, body any, dst any) error {
	req, err := c.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	return c.do(req, dst)
}

// doGet sends a GET request to the given path, decoding the response into dst.
func (c *Client) doGet(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, dst)
}

// doRaw sends a request with the given method, path, body, and content type,
// returning the raw response bytes and the response content type.
func (c *Client) doRaw(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, "", err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", c.makeError(resp, data)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// doMultipart sends a multipart POST request to the given path, decoding the response into dst.
func (c *Client) doMultipart(ctx context.Context, path string, body io.Reader, contentType string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	return c.do(req, dst)
}

// do executes the request, reads the body, checks the status, and unmarshals into dst.
func (c *Client) do(req *http.Request, dst any) error {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return c.makeError(resp, data)
	}
	if dst != nil {
		if err := json.Unmarshal(data, dst); err != nil {
			return fmt.Errorf("runjobs: decode response: %w", err)
		}
	}
	return nil
}

// makeError constructs an *APIError from a failed HTTP response.
//
// The upstream gateway is *supposed* to return `{"error": "<string>"}`, but
// real-world responses (especially when the gateway transparently bubbles up
// a provider's error envelope) come in several shapes:
//
//	{"error": "plain string"}                            // ideal
//	{"error": {"message": "...", "code": "...", ...}}    // OpenAI / Ark style
//	{"error": {"error": {"message": "..."}}}             // doubly nested
//	non-JSON HTML / plain text body                      // upstream proxy
//
// Older versions of this function only handled the first form, so any
// structured error silently became `APIError{Message:""}` and the caller saw
// `runjobs: 502 gateway_error: ` with no detail. We now try harder, falling
// back to the raw body so callers always have *something* actionable.
func (c *Client) makeError(resp *http.Response, body []byte) error {
	return &APIError{
		StatusCode: resp.StatusCode,
		Type:       "gateway_error",
		Message:    extractErrorMessage(body),
	}
}

// extractErrorMessage pulls a human-readable message out of an error body,
// regardless of whether `error` is a string, an object with `message`/`code`,
// or absent entirely.
func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var raw struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err == nil && len(raw.Error) > 0 {
		if msg := messageFromRaw(raw.Error); msg != "" {
			return msg
		}
	}
	// Body wasn't JSON or had no `error` field — surface it raw, but keep it
	// bounded so an HTML error page doesn't blow up downstream prompts.
	const maxRawLen = 2048
	if len(body) > maxRawLen {
		return string(body[:maxRawLen]) + "…(truncated)"
	}
	return string(body)
}

// messageFromRaw inspects a JSON value sitting at the `error` key and tries to
// turn it into a string. Handles plain strings, {message,code} objects, and
// one level of nesting (`{"error":{"error":{...}}}`).
func messageFromRaw(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Message string          `json:"message"`
		Code    string          `json:"code"`
		Type    string          `json:"type"`
		Param   string          `json:"param"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if obj.Message != "" {
		out := obj.Message
		if obj.Code != "" {
			out = obj.Code + ": " + out
		}
		if obj.Param != "" {
			out += " (param=" + obj.Param + ")"
		}
		return out
	}
	if len(obj.Error) > 0 {
		return messageFromRaw(obj.Error)
	}
	return ""
}

// imagePollFirstDelay is the delay before the first poll after job submission.
// imagePollInterval is the delay between subsequent polls.
// Both are package-scope variables so tests can override them.
var (
	imagePollFirstDelay = 2 * time.Second
	imagePollInterval   = 2 * time.Second
)

type imageJobResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   []struct {
		// URL is the only carrier for image bytes — either an
		// "https://<gateway>/v1/blobs/<id>" hosted blob (async path,
		// the typical case) OR a "data:<mime>;base64,..." URI
		// (sync-style passthroughs where bytes ride inline). Both
		// shapes are handled identically by DecodeMediaURL and by
		// SDK consumers — the legacy b64_json sibling has been
		// dropped, mirroring the gateway's URL-only response shape.
		URL           string `json:"url,omitempty"`
		Size          string `json:"size,omitempty"`
		RevisedPrompt string `json:"revised_prompt,omitempty"`
		// Attribution is the credit line stock-photo providers (Pexels)
		// require alongside displayed images. Empty for AI-generated
		// results. See ImageResult.Attribution for full semantics.
		Attribution string `json:"attribution,omitempty"`
	} `json:"data,omitempty"`
	Usage imageJobUsage `json:"usage"`
}

type imageJobUsage struct {
	TotalCost       float64        `json:"total_cost"`
	GeneratedImages int            `json:"generated_images,omitempty"`
	OutputTokens    int            `json:"output_tokens,omitempty"`
	TotalTokens     int            `json:"total_tokens,omitempty"`
	ToolUsage       map[string]int `json:"tool_usage,omitempty"`
}

// waitImageJob polls statusPath until the job reaches a terminal state, then
// returns the assembled *ImageResponse. It respects ctx cancellation; if ctx
// has no deadline, a 10-minute internal timeout is applied.
func (c *Client) waitImageJob(ctx context.Context, statusPath string) (*ImageResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}

	timer := time.NewTimer(imagePollFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}

		var status imageJobResponse
		if err := c.doGet(ctx, statusPath, &status); err != nil {
			return nil, err
		}
		switch status.Status {
		case "succeeded":
			return c.assembleImageResponse(ctx, &status)
		case "failed":
			return nil, &APIError{StatusCode: 502, Type: "image_job_failed", Message: status.Error}
		case "queued", "running":
			timer.Reset(imagePollInterval)
		default:
			return nil, fmt.Errorf("runjobs: unknown job status %q", status.Status)
		}
	}
}

// assembleImageResponse converts a succeeded imageJobResponse into an
// *ImageResponse. The URL field is passed through unchanged — callers
// that want decoded bytes call DecodeMediaURL on each Data[i].URL.
// This keeps the response cheap (no eager download of every blob)
// while preserving symmetry with the gateway's URL-only wire shape.
func (c *Client) assembleImageResponse(ctx context.Context, status *imageJobResponse) (*ImageResponse, error) {
	_ = ctx // reserved for future per-result eager fetches if needed
	out := &ImageResponse{
		Created: time.Now().Unix(),
		Data:    make([]ImageResult, len(status.Data)),
		Usage: ImageUsage{
			TotalCost:       status.Usage.TotalCost,
			GeneratedImages: status.Usage.GeneratedImages,
			OutputTokens:    status.Usage.OutputTokens,
			TotalTokens:     status.Usage.TotalTokens,
			ToolUsage:       status.Usage.ToolUsage,
		},
	}
	for i, d := range status.Data {
		out.Data[i] = ImageResult{
			URL:           d.URL,
			Size:          d.Size,
			RevisedPrompt: d.RevisedPrompt,
			Attribution:   d.Attribution,
		}
	}
	return out, nil
}
