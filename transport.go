package runjobs

import (
	"bytes"
	"context"
	"encoding/base64"
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
		URL string `json:"url,omitempty"`
		// B64JSON is an optional inline result. When populated,
		// assembleImageResponse uses it directly and skips the
		// URL download. Middle-proxy gateways (e.g. runjobs-backend
		// wrapping ai-gateway) that already have the bytes in
		// memory set this so they don't need to run their own
		// blob store just to satisfy waitImageJob's URL contract.
		// ai-gateway itself still emits URL only — the poll
		// response there is small and links to a streamable blob
		// endpoint by design.
		B64JSON       string `json:"b64_json,omitempty"`
		Size          string `json:"size,omitempty"`
		RevisedPrompt string `json:"revised_prompt,omitempty"`
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
// *ImageResponse. Each result is either used from the inline
// b64_json field (preferred) or downloaded from the url field as a
// fallback. Inline b64 lets a middle-proxy that already has the
// bytes skip a second HTTP hop, while the url path keeps ai-gateway's
// design — small poll response, blob streamed separately — intact.
func (c *Client) assembleImageResponse(ctx context.Context, status *imageJobResponse) (*ImageResponse, error) {
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
		b64 := d.B64JSON
		if b64 == "" {
			var err error
			b64, err = c.downloadBlobAsB64(ctx, d.URL)
			if err != nil {
				return nil, fmt.Errorf("runjobs: fetch result image %d: %w", i, err)
			}
		}
		out.Data[i] = ImageResult{
			B64JSON:       b64,
			Size:          d.Size,
			RevisedPrompt: d.RevisedPrompt,
		}
	}
	return out, nil
}

// downloadBlobAsB64 fetches the blob at the given full URL and returns it as
// a base64-encoded string. The Authorization header is set for uniformity even
// though the gateway's blob endpoint is unauthenticated.
func (c *Client) downloadBlobAsB64(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("blob download: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
