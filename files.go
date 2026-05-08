package runjobs

// FilesService gives the SDK access to the per-project file system at
// /v1/files/*.  Files are stored under (user, project) on the gateway
// and addressed by POSIX-style paths.  Every Object's URL is a stable
// public address — embed it in `<img>`, share it, persist it.
//
// All methods require the client's API key to be a project-bound
// resource token (rrt_*) or a hardware-container token whose project
// instance the gateway can resolve.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FilesService is the entry point for /v1/files operations.
type FilesService struct {
	client *Client
}

// FileObject is the shape returned by Put / Get / List / Stat / Move / Copy.
type FileObject struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	ContentType  string    `json:"content_type,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	LastModified time.Time `json:"last_modified,omitempty"`
	URL          string    `json:"url"`
}

// FileListResult is one page of List output.
type FileListResult struct {
	Files      []FileObject `json:"files"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// PutOptions modifies Put behaviour.
type PutOptions struct {
	// ContentType overrides the MIME type guessed from the path
	// extension.  Optional; defaults to a sensible guess.
	ContentType string
	// IfNoneMatch=true asks the gateway to refuse if the path already
	// exists.  Returns *APIError with StatusCode 409 on collision.
	IfNoneMatch bool
}

// ListOptions modifies List behaviour.
type ListOptions struct {
	Prefix string
	Cursor string
	Limit  int
}

func (s *FilesService) pathToURL(p string) string {
	// Encode each segment so spaces / unicode survive the round trip.
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return "/v1/files/" + strings.Join(parts, "/")
}

// Put uploads body to path.  Returns the resulting FileObject (with
// stable public URL).  Honours opts.IfNoneMatch.
func (s *FilesService) Put(ctx context.Context, p string, body []byte, opts ...PutOptions) (*FileObject, error) {
	var o PutOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	contentType := o.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.client.baseURL+s.pathToURL(p), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if o.IfNoneMatch {
		req.Header.Set("If-None-Match", "*")
	}
	var obj FileObject
	if err := s.client.do(req, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// PutString is a convenience wrapper around Put for text content.
func (s *FilesService) PutString(ctx context.Context, p, content string, opts ...PutOptions) (*FileObject, error) {
	return s.Put(ctx, p, []byte(content), opts...)
}

// Get retrieves the bytes at path.  Returns (nil, "", err) on failure.
func (s *FilesService) Get(ctx context.Context, p string) ([]byte, string, error) {
	return s.client.doRaw(ctx, http.MethodGet, s.pathToURL(p), nil, "")
}

// Stat returns metadata for path.  HEAD-only — no body fetch.
func (s *FilesService) Stat(ctx context.Context, p string) (*FileObject, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.client.baseURL+s.pathToURL(p), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.client.apiKey)
	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, &APIError{StatusCode: 404, Type: "not_found", Message: "not found"}
	}
	if resp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: resp.StatusCode, Type: "gateway_error", Message: "stat failed"}
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return &FileObject{
		Path:        resp.Header.Get("X-File-Path"),
		Size:        size,
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        strings.Trim(resp.Header.Get("ETag"), "\""),
		URL:         resp.Header.Get("X-File-URL"),
	}, nil
}

// Exists returns true iff path resolves to an object.
func (s *FilesService) Exists(ctx context.Context, p string) (bool, error) {
	_, err := s.Stat(ctx, p)
	if err == nil {
		return true, nil
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

// Delete removes the object at path.  Idempotent — deleting a
// non-existent path returns nil.
func (s *FilesService) Delete(ctx context.Context, p string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.client.baseURL+s.pathToURL(p), nil)
	if err != nil {
		return err
	}
	return s.client.do(req, nil)
}

// List enumerates objects.  Pass an empty prefix for the root.
func (s *FilesService) List(ctx context.Context, opts ...ListOptions) (*FileListResult, error) {
	var o ListOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	q := url.Values{}
	if o.Prefix != "" {
		q.Set("prefix", o.Prefix)
	}
	if o.Cursor != "" {
		q.Set("cursor", o.Cursor)
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	path := "/v1/files"
	if qs := q.Encode(); qs != "" {
		path += "?" + qs
	}
	var res FileListResult
	if err := s.client.doGet(ctx, path, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Move renames an object atomically (copy + delete server-side).
func (s *FilesService) Move(ctx context.Context, from, to string) (*FileObject, error) {
	body := map[string]string{"from": from, "to": to}
	var obj FileObject
	if err := s.client.doJSON(ctx, "/v1/files/move", body, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// Copy duplicates an object without removing the source.
func (s *FilesService) Copy(ctx context.Context, from, to string) (*FileObject, error) {
	body := map[string]string{"from": from, "to": to}
	var obj FileObject
	if err := s.client.doJSON(ctx, "/v1/files/copy", body, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// PutFromURLOptions configures PutFromURL.
type PutFromURLOptions struct {
	ContentType string
	IfNoneMatch bool
}

// PutFromURL asks the gateway to fetch srcURL server-side and store
// the result at path.  Useful for ingesting an existing public asset
// without round-tripping the bytes through the SDK.  srcURL must be
// a public http(s) URL — private / loopback / metadata hosts are
// rejected.
func (s *FilesService) PutFromURL(ctx context.Context, p, srcURL string, opts ...PutFromURLOptions) (*FileObject, error) {
	var o PutFromURLOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	body := map[string]any{
		"path":          p,
		"src_url":       srcURL,
		"content_type":  o.ContentType,
		"if_none_match": o.IfNoneMatch,
	}
	var obj FileObject
	if err := s.client.doJSON(ctx, "/v1/files/put-url", body, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// BatchOp is one operation in a Batch call.  Op must be one of:
// "put_url" | "del" | "move" | "copy" | "exists" | "stat".
type BatchOp struct {
	Op          string `json:"op"`
	Path        string `json:"path,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	SrcURL      string `json:"src_url,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	IfNoneMatch bool   `json:"if_none_match,omitempty"`
}

// BatchResult mirrors the per-op response.  OK is true on success;
// Object is set for ops that return one (put_url, move, copy, stat);
// Exists is set for "exists" ops; Error carries the failure message.
type BatchResult struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error,omitempty"`
	Object *FileObject `json:"object,omitempty"`
	Exists *bool       `json:"exists,omitempty"`
}

// Batch executes ops sequentially in one round trip.  One op failing
// does not abort the rest — inspect each BatchResult.OK individually.
func (s *FilesService) Batch(ctx context.Context, ops []BatchOp) ([]BatchResult, error) {
	body := map[string]any{"ops": ops}
	var resp struct {
		Results []BatchResult `json:"results"`
	}
	if err := s.client.doJSON(ctx, "/v1/files/batch", body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// URL returns the public URL for path *without* contacting the gateway.
// It mirrors the gateway's namespacing rule, but only if the SDK's
// baseURL is configured with the same files origin.  Most callers
// should use the URL returned by Put / Stat / List instead, which is
// the authoritative value.
//
// This helper is intentionally limited — it doesn't validate the path,
// percent-encode beyond simple URL escape, or know about the project
// scope, since those live on the server.  Use it only when you've
// already received a path back from the gateway and want the URL form.
func (s *FilesService) urlOf(rawPath string, baseFiles string, userID, projectID string) string {
	if baseFiles == "" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(rawPath, "/"), "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return fmt.Sprintf("%s/userfiles/u/%s/p/%s/%s", strings.TrimRight(baseFiles, "/"), userID, projectID, strings.Join(parts, "/"))
}

// readBytesAt is unused but kept to make sure encoding/json + io
// remain referenced even when the package is built without tests
// — silences unused-import warnings during incremental refactors.
var _ = io.EOF
var _ = json.Valid
