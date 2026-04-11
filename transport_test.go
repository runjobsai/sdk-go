package runjobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": "internal failure"}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	var dst map[string]any
	err := c.doJSON(context.Background(), "/v1/test", map[string]string{"key": "val"}, &dst)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "internal failure" {
		t.Fatalf("expected message 'internal failure', got %q", apiErr.Message)
	}
}

func TestDoGetSetsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok": true}`)
	}))
	defer srv.Close()

	c := NewClient("gw-secret", WithBaseURL(srv.URL))
	var dst map[string]any
	err := c.doGet(context.Background(), "/v1/check", &dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer gw-secret" {
		t.Fatalf("expected 'Bearer gw-secret', got %q", gotAuth)
	}
}

func TestDoRawError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error": "forbidden"}`)
	}))
	defer srv.Close()

	c := NewClient("gw-key", WithBaseURL(srv.URL))
	_, _, err := c.doRaw(context.Background(), "GET", "/v1/raw", nil, "")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", apiErr.StatusCode)
	}
}

func TestMakeErrorWithMalformedJSON(t *testing.T) {
	c := NewClient("gw-key")
	resp := &http.Response{StatusCode: 502}
	err := c.makeError(resp, []byte("not json"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 502 {
		t.Fatalf("expected 502, got %d", apiErr.StatusCode)
	}
	// Non-JSON bodies now fall back to raw text so callers see *something*
	// instead of a bare "runjobs: 502 gateway_error: ".
	if apiErr.Message != "not json" {
		t.Fatalf("expected raw body fallback, got %q", apiErr.Message)
	}
}

func TestMakeErrorEmptyBody(t *testing.T) {
	c := NewClient("gw-key")
	resp := &http.Response{StatusCode: 502}
	err := c.makeError(resp, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "" {
		t.Fatalf("expected empty message for empty body, got %v", err)
	}
}

func TestMakeErrorStringErrorField(t *testing.T) {
	c := NewClient("gw-key")
	resp := &http.Response{StatusCode: 500}
	err := c.makeError(resp, []byte(`{"error":"plain string"}`))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "plain string" {
		t.Fatalf("expected 'plain string', got %v", err)
	}
}

func TestMakeErrorObjectErrorField(t *testing.T) {
	c := NewClient("gw-key")
	resp := &http.Response{StatusCode: 400}
	body := []byte(`{"error":{"code":"InvalidParameter","message":"first/last frame content cannot be mixed with reference media content","param":"content","type":"BadRequest"}}`)
	err := c.makeError(resp, body)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	want := "InvalidParameter: first/last frame content cannot be mixed with reference media content (param=content)"
	if apiErr.Message != want {
		t.Fatalf("expected %q, got %q", want, apiErr.Message)
	}
}

func TestMakeErrorNestedErrorField(t *testing.T) {
	c := NewClient("gw-key")
	resp := &http.Response{StatusCode: 502}
	body := []byte(`{"error":{"error":{"message":"inner detail","code":"DEEP"}}}`)
	err := c.makeError(resp, body)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "DEEP: inner detail" {
		t.Fatalf("expected nested unwrap, got %v", err)
	}
}

func TestMakeErrorTruncatesLongRawBody(t *testing.T) {
	c := NewClient("gw-key")
	resp := &http.Response{StatusCode: 502}
	body := make([]byte, 4096)
	for i := range body {
		body[i] = 'x'
	}
	err := c.makeError(resp, body)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if len(apiErr.Message) > 2100 { // 2048 + ellipsis suffix
		t.Fatalf("expected truncated message, got len=%d", len(apiErr.Message))
	}
	if !strings.HasSuffix(apiErr.Message, "(truncated)") {
		t.Fatalf("expected truncation marker, got %q", apiErr.Message[len(apiErr.Message)-20:])
	}
}
