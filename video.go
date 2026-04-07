package runjobs

import (
	"context"
	"fmt"
	"time"
)

// VideoService provides access to the gateway's video endpoints.
type VideoService struct {
	client *Client
}

// VideoGenerateParams configures a video generation request.
type VideoGenerateParams struct {
	Prompt        string `json:"prompt"`
	AspectRatio   string `json:"aspect_ratio,omitempty"`
	Duration      int    `json:"duration,omitempty"`
	Resolution    string `json:"resolution,omitempty"`
	GenerateAudio bool   `json:"generate_audio,omitempty"`
	FirstFrameB64 string `json:"first_frame_b64,omitempty"`
	LastFrameB64  string `json:"last_frame_b64,omitempty"`
	User          string `json:"user,omitempty"`
}

// VideoTask is the response from a video generation request.
type VideoTask struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Usage  Usage  `json:"usage"`
}

// VideoStatus represents the status of a video generation task.
type VideoStatus struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	VideoURL string `json:"video_url,omitempty"`
	Error    string `json:"error,omitempty"`
}

// PollOption configures polling behaviour for Wait.
type PollOption func(*pollConfig)

type pollConfig struct {
	interval time.Duration
}

// WithPollInterval sets the polling interval for Wait.
func WithPollInterval(d time.Duration) PollOption {
	return func(c *pollConfig) { c.interval = d }
}

// Generate starts a video generation task.
func (s *VideoService) Generate(ctx context.Context, model string, params VideoGenerateParams) (*VideoTask, error) {
	body := struct {
		Model string `json:"model"`
		VideoGenerateParams
	}{Model: model, VideoGenerateParams: params}

	var task VideoTask
	if err := s.client.doJSON(ctx, "/v1/videos/generations", body, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetStatus retrieves the current status of a video generation task.
func (s *VideoService) GetStatus(ctx context.Context, taskID string) (*VideoStatus, error) {
	var status VideoStatus
	if err := s.client.doGet(ctx, "/v1/videos/generations/"+taskID, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// Wait polls GetStatus until the task reaches "succeeded" or "failed".
// The default poll interval is 5 seconds; use WithPollInterval to override.
func (s *VideoService) Wait(ctx context.Context, taskID string, opts ...PollOption) (*VideoStatus, error) {
	cfg := pollConfig{interval: 5 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}

	for {
		status, err := s.GetStatus(ctx, taskID)
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case "succeeded", "failed":
			return status, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("runjobs: wait cancelled: %w", ctx.Err())
		case <-time.After(cfg.interval):
		}
	}
}

// GetContent retrieves the raw video bytes for a completed task.
func (s *VideoService) GetContent(ctx context.Context, taskID string) (data []byte, mime string, err error) {
	return s.client.doRaw(ctx, "GET", "/v1/videos/"+taskID+"/content", nil, "")
}
