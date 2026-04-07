package runjobs

import "net/http"

const defaultBaseURL = "http://localhost:8081"

// Client is the top-level RunJobs SDK client.
type Client struct {
	Chat     *ChatService
	Models   *ModelService
	Image    *ImageService
	Audio    *AudioService
	Video    *VideoService
	Computer *ComputerService

	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithBaseURL overrides the default gateway base URL.
func WithBaseURL(url string) ClientOption { return func(c *Client) { c.baseURL = url } }

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption { return func(c *Client) { c.httpClient = hc } }

// NewClient creates a new RunJobs SDK client. The apiKey is the gateway API key
// (typically prefixed "gw-"). Options may override the base URL and HTTP client.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
	for _, o := range opts {
		o(c)
	}

	c.Chat = &ChatService{client: c}
	c.Models = &ModelService{client: c}
	c.Image = &ImageService{client: c}
	c.Audio = &AudioService{client: c}
	c.Video = &VideoService{client: c}
	c.Computer = &ComputerService{client: c}
	return c
}
