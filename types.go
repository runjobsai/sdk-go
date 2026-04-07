package runjobs

// Usage carries the gateway's cost information returned with every response.
type Usage struct {
	TotalCost float64 `json:"total_cost"`
}
