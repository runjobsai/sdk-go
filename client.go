package runjobs

import (
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const defaultBaseURL = "http://localhost:8081"

// Client is the top-level RunJobs SDK client. It wraps the OpenAI client for
// chat completions and provides additional services for models, images, audio,
// and video.
type Client struct {
	Chat   *openai.ChatCompletionService
	Models *ModelService
	Image  *ImageService
	Audio  *AudioService
	Video  *VideoService

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

	oaiOpts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(c.baseURL + "/v1/"),
	}
	if c.httpClient != http.DefaultClient {
		oaiOpts = append(oaiOpts, option.WithHTTPClient(c.httpClient))
	}
	oai := openai.NewClient(oaiOpts...)
	c.Chat = &oai.Chat.Completions
	c.Models = &ModelService{client: c}
	c.Image = &ImageService{client: c}
	c.Audio = &AudioService{client: c}
	c.Video = &VideoService{client: c}
	return c
}
