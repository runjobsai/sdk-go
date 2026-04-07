package runjobs

import (
	"net/http"
	"testing"
)

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("gw-test-key")
	if c.baseURL != "http://localhost:8081" {
		t.Fatalf("expected default base URL, got %s", c.baseURL)
	}
	if c.apiKey != "gw-test-key" {
		t.Fatalf("expected apiKey gw-test-key, got %s", c.apiKey)
	}
	if c.httpClient != http.DefaultClient {
		t.Fatal("expected default HTTP client")
	}
	if c.Chat == nil || c.Models == nil || c.Image == nil || c.Audio == nil || c.Video == nil || c.Computer == nil {
		t.Fatal("expected all services to be initialized")
	}
}

func TestWithBaseURL(t *testing.T) {
	c := NewClient("gw-key", WithBaseURL("https://example.com"))
	if c.baseURL != "https://example.com" {
		t.Fatalf("expected https://example.com, got %s", c.baseURL)
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 42}
	c := NewClient("gw-key", WithHTTPClient(custom))
	if c.httpClient != custom {
		t.Fatal("expected custom HTTP client")
	}
}
