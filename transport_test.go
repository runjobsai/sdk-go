package runjobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	// Message should be empty when JSON parse fails
	if apiErr.Message != "" {
		t.Fatalf("expected empty message, got %q", apiErr.Message)
	}
}
