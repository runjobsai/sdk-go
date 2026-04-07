package runjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/openai/openai-go/v3"
)

// doJSON sends a JSON POST request to the given path, decoding the response into dst.
func (c *Client) doJSON(ctx context.Context, path string, body any, dst any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("runjobs: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
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
		return nil, "", c.makeError(req, resp, data)
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
		return c.makeError(req, resp, data)
	}
	if dst != nil {
		if err := json.Unmarshal(data, dst); err != nil {
			return fmt.Errorf("runjobs: decode response: %w", err)
		}
	}
	return nil
}

// makeError constructs an *openai.Error from a failed HTTP response.
// The gateway returns {"error": "message"} on failure.
func (c *Client) makeError(req *http.Request, resp *http.Response, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)

	apiErr := openai.Error{
		Type:       "gateway_error",
		Message:    payload.Error,
		StatusCode: resp.StatusCode,
		Request:    req,
		Response:   resp,
	}
	return &apiErr
}
