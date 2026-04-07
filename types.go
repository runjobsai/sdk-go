package runjobs

import (
	"strconv"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
)

// Usage carries the gateway's cost information returned with every response.
type Usage struct {
	TotalCost float64 `json:"total_cost"`
}

// ChatCost extracts the gateway's total_cost (USD) from an openai-go chat
// completion response. The gateway injects total_cost into the usage object
// as an extra field. Returns 0 if the field is not present.
func ChatCost(resp *openai.ChatCompletion) float64 {
	if resp == nil {
		return 0
	}
	return extractCost(resp.Usage.JSON.ExtraFields)
}

// StreamCost extracts the gateway's total_cost (USD) from an openai-go chat
// completion chunk (the final chunk with usage). Returns 0 if not present.
func StreamCost(chunk openai.ChatCompletionChunk) float64 {
	return extractCost(chunk.Usage.JSON.ExtraFields)
}

func extractCost(fields map[string]respjson.Field) float64 {
	if fields == nil {
		return 0
	}
	f, ok := fields["total_cost"]
	if !ok {
		return 0
	}
	raw := f.Raw()
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}
