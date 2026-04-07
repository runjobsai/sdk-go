package runjobs

import "fmt"

// APIError represents an error returned by the RunJobs gateway.
type APIError struct {
	StatusCode int    `json:"-"`
	Type       string `json:"type"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("runjobs: %d %s: %s", e.StatusCode, e.Type, e.Message)
}
