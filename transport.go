package runjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
