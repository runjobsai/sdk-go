package runjobs

import "testing"

func TestAPIErrorFormat(t *testing.T) {
	e := &APIError{StatusCode: 429, Type: "gateway_error", Message: "rate limited"}
	got := e.Error()
	want := "runjobs: 429 gateway_error: rate limited"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAPIErrorImplementsError(t *testing.T) {
	var err error = &APIError{StatusCode: 500, Type: "gateway_error", Message: "boom"}
	if err.Error() == "" {
		t.Fatal("expected non-empty error string")
	}
}
