package runjobs

// Usage carries token consumption and cost information returned with every response.
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	TotalCost        float64 `json:"total_cost"`
}
