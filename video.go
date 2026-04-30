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

// VideoGenerateParams configures a video generation request. Most fields
// map 1:1 to the gateway's `/v1/videos/generations` body; pointer-typed
// flags (Watermark, CameraFixed, ReturnLastFrame, Draft) are tri-state so
// `false` is distinguishable from "unset, use upstream default".
type VideoGenerateParams struct {
	Prompt        string `json:"prompt"`
	AspectRatio   string `json:"aspect_ratio,omitempty"`
	Duration      int    `json:"duration,omitempty"`
	Resolution    string `json:"resolution,omitempty"`
	GenerateAudio *bool  `json:"generate_audio,omitempty"`
	// First/last frame keyframes (image-to-video). Either a publicly
	// hosted URL or raw base64 — the gateway stashes b64 inputs as
	// short-lived blobs so the upstream sees a URL.
	FirstFrameURL string `json:"first_frame_url,omitempty"`
	LastFrameURL  string `json:"last_frame_url,omitempty"`
	FirstFrameB64 string `json:"first_frame_b64,omitempty"`
	LastFrameB64  string `json:"last_frame_b64,omitempty"`
	// Multimodal reference inputs (Seedance 2.0): up to 9 reference
	// images, up to 3 reference videos (≤15s total), up to 3 reference
	// audios (≤15s total). Reference images may be data: URIs; the
	// gateway will materialise them. Reference videos / audios must be
	// hosted URLs (or data URIs that the gateway can stash).
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
	ReferenceImagesB64 []string `json:"reference_images_b64,omitempty"`
	ReferenceVideoURLs []string `json:"reference_video_urls,omitempty"`
	ReferenceAudioURLs []string `json:"reference_audio_urls,omitempty"`
	// SourceVideoURL is the *single* clip to be edited by a video-edit
	// model (Aliyun wan2.7-videoedit). Distinct from ReferenceVideoURLs
	// (Seedance "match motion / cinematography of these clips" — multiple,
	// content untouched): the source video's content gets MODIFIED per
	// the prompt + reference images, with timing / camera / audio
	// preserved. Must be a publicly-reachable HTTP(S) URL (data: URIs
	// not currently supported on this field).
	SourceVideoURL string `json:"source_video_url,omitempty"`
	// Output spec knobs.
	Watermark       *bool `json:"watermark,omitempty"`
	CameraFixed     *bool `json:"camera_fixed,omitempty"`
	ReturnLastFrame *bool `json:"return_last_frame,omitempty"`
	Seed            int64 `json:"seed,omitempty"`
	// Frames is an alternative to Duration for Seedance 1.0 pro / lite
	// (range [29, 289], step 25 + 4n).
	Frames int `json:"frames,omitempty"`
	// Draft / DraftTaskID enable Seedance 1.5 pro Draft Mode. Set
	// Draft=true on Step1 to get a cheap preview; Step2 references the
	// Step1 task id via DraftTaskID to promote the draft to a final video.
	Draft       *bool  `json:"draft,omitempty"`
	DraftTaskID string `json:"draft_task_id,omitempty"`
	// ServiceTier "flex" switches to offline inference (~50% price);
	// pair with ExecutionExpiresAfter to bound the queue time. "default"
	// is the standard online tier.
	ServiceTier           string `json:"service_tier,omitempty"`
	ExecutionExpiresAfter int64  `json:"execution_expires_after,omitempty"`
	// CallbackURL receives a POST when the task transitions states. The
	// payload mirrors GetStatus's response shape.
	CallbackURL string `json:"callback_url,omitempty"`
	User        string `json:"user,omitempty"`
}

// VideoTask is the response from a video generation request.
type VideoTask struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Usage  Usage  `json:"usage"`
}

// VideoStatus represents the status of a video generation task. The optional
// metadata fields (LastFrameURL onwards) are only populated when the task is
// in a terminal state (`succeeded`).
type VideoStatus struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	VideoURL string `json:"video_url,omitempty"`
	Error    string `json:"error,omitempty"`
	// LastFrameURL is the final frame as a separate image URL —
	// populated when the original request set ReturnLastFrame=true.
	LastFrameURL string `json:"last_frame_url,omitempty"`
	// DraftTaskID is the Seedance Draft Mode Step1 result: when a draft
	// task succeeds, the gateway exposes its id so the caller can pass
	// it back as DraftTaskID on a follow-up Step2 request.
	DraftTaskID string `json:"draft_task_id,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	FPS         int    `json:"fps,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Ratio       string `json:"ratio,omitempty"`
	Seed        int64  `json:"seed,omitempty"`
	ServiceTier string `json:"service_tier,omitempty"`
	// UsageTokens is the upstream-reported token consumption (Seedance
	// charges per output token). Independent of the gateway billing
	// total in VideoTask.Usage.TotalCost.
	UsageTokens *VideoUsageTokens `json:"usage_tokens,omitempty"`
	CreatedAt   int64             `json:"created_at,omitempty"`
	UpdatedAt   int64             `json:"updated_at,omitempty"`
}

// VideoUsageTokens carries the upstream-reported token counts surfaced on
// terminal video status responses.
type VideoUsageTokens struct {
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
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
