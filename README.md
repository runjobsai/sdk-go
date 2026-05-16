# RunJobs SDK for Go

Go client for the [RunJobs AI Gateway](https://github.com/runjobsai/ai-gateway). Zero external dependencies.

Covers every public endpoint the gateway exposes — chat, embeddings, image, audio (TTS + STT), video, computer use, the `/v1/files` per-project file system, and the typed model-options schema that drives both UI rendering and pre-flight validation.

## Install

```bash
go get github.com/runjobsai/sdk-go
```

## Quick Start

```go
package main

import (
    "context"
    "errors"
    "fmt"

    runjobs "github.com/runjobsai/sdk-go"
)

func main() {
    client := runjobs.NewClient("gw-your-api-key",
        runjobs.WithBaseURL("https://api.runjobs.ai"),
    )
    ctx := context.Background()

    resp, err := client.Chat.New(ctx, runjobs.ChatCompletionParams{
        Model: "Claude Haiku 4.5",
        Messages: []runjobs.ChatMessage{
            {Role: "user", Content: "Hello!"},
        },
    })
    if err != nil {
        var apiErr *runjobs.APIError
        if errors.As(err, &apiErr) {
            fmt.Printf("API error %d: %s\n", apiErr.StatusCode, apiErr.Message)
        }
        return
    }
    fmt.Println(resp.Choices[0].Message.Content)
    fmt.Printf("Cost: $%.6f\n", resp.Usage.TotalCost)
}
```

## Client

```go
client := runjobs.NewClient(apiKey,
    runjobs.WithBaseURL("https://api.runjobs.ai"),  // optional override
    runjobs.WithHTTPClient(myHTTPClient),           // optional custom *http.Client
)
```

API keys come in three prefixes — all accepted via the same constructor:

| Prefix  | Scope                                    | Source                                |
|---------|------------------------------------------|---------------------------------------|
| `gw-…`  | User-level gateway key                   | Dashboard → Settings → API keys       |
| `rj_…`  | Workspace agent token                    | Provisioned by the workspace runtime  |
| `rrt_…` | Project-bound resource token (files API) | `/api/sdk/grant` for browser bundles  |

The `client.Files` service requires an `rrt_*` (or hardware-container) token so the gateway can resolve a project. The other services work with any of the three.

## Services

### `client.Models`

```go
// Full catalog.
models, _ := client.Models.List(ctx)

// Filter server-side by capability bucket.
videoModels, _ := client.Models.List(ctx, runjobs.WithCapability("video_generation"))
```

Each `Model` carries pricing (`InputPricePerMTok`, `OutputPricePerMTok`, both in **pips per million tokens**; 1 USD = 1,000,000 pips), capability tags, and the typed input schema.

```go
for _, m := range videoModels {
    var tagIDs []string
    for _, t := range m.CapabilityTags {
        tagIDs = append(tagIDs, t.ID)
    }
    fmt.Printf("%-30s [%s]  in=%d out=%d pips/MTok\n",
        m.ID, strings.Join(tagIDs, ","),
        m.InputPricePerMTok, m.OutputPricePerMTok)
}

if m.HasCapabilityTag("first_last_frame") { /* show keyframe uploader */ }
```

**Capability tag vocabulary** — stable IDs, safe to filter on:

| Capability         | Tag IDs |
|--------------------|---------|
| `video_generation` | `t2v`, `i2v`, `v2v`, `a2v`, `first_last_frame`, `reference`, `motion_transfer`, `audio_track` |
| `image_generation` | `t2i`, `i2i`, `inpaint` |
| `text_to_speech`   | `tts`, `voice_clone`, `instruct`, `emotion`, `voice_catalog` |
| `speech_to_text`   | `stt`, `timestamps` |
| `embedding`        | `embedding` |
| `text` / `vision`  | `chat`, `vision` |
| `computer_use`     | `computer_use` |

`Tag.Label` is English-only; localise on your side. Always filter on `Tag.ID`.

### Options schema — per-model field contract

`m.OptionsSchema()` returns the typed view of the model's input contract: which fields are accepted, their bounds / enums / defaults, cross-field constraints (XOR groups, requires-all, pixel bounds), plus a `Catalog` of rich content (voices, emotions) that an enum alone can't express.

```go
list, _ := client.Models.List(ctx, runjobs.WithCapability("text_to_speech"))
var cosy runjobs.Model
for _, m := range list { if m.ID == "CosyVoice" { cosy = m } }

schema, _ := cosy.OptionsSchema()                       // *runjobs.Schema, nil if no options

cosy.AcceptsField("reference_audio_url")                // bool — render UI chip?
cosy.RequiresField("source_audio_url")                  // bool — red-star this field?
cosy.AllowedValuesFor("emotion")                        // []any — dropdown enum, or nil

if schema != nil && schema.Catalog != nil {
    for _, v := range schema.Catalog.Voices {
        fmt.Printf("%s  %s  %s\n", v["id"], v["name"], v["gender"])
    }
    fmt.Println("Emotions:", schema.Catalog.Emotions)
}

// Convenience: top-level voice id list (no metadata).
fmt.Println("Voice IDs:", cosy.AvailableVoices)
```

**Pre-flight validation** — catches missing required fields, out-of-range numbers, mutex violations, pixel-bounds violations before the gateway 400s:

```go
errs := schema.ValidateRequest(map[string]any{
    "input": "Hello",
    "voice": "alloy",
    "reference_audio_url": "https://x/sample.wav",       // mutex violation
})
for _, e := range errs { fmt.Printf("%s: %s\n", e.Field, e.Reason) }
```

Constraint kinds the validator understands: `any_of_required`, `mutually_exclusive`, `group_mutex` (block-level XOR — e.g. keyframe block XOR reference block), `requires_all`, `pixel_bounds`. Unknown kinds are skipped (forward-compatible).

### `client.Chat`

OpenAI-compatible — same wire format, same semantics. Streaming and non-streaming.

```go
resp, _ := client.Chat.New(ctx, runjobs.ChatCompletionParams{
    Model: "Claude Sonnet 4.6",
    Messages: []runjobs.ChatMessage{
        {Role: "user", Content: "Explain Go interfaces in one sentence."},
    },
})
fmt.Println(resp.Choices[0].Message.Content)
fmt.Printf("Cost: $%.6f\n", resp.Usage.TotalCost)
```

**Streaming** — iterator pattern. The final chunk carries `Usage` (forced on via `stream_options.include_usage`):

```go
stream := client.Chat.NewStreaming(ctx, runjobs.ChatCompletionParams{
    Model:    "Gemini 3 Flash",
    Messages: []runjobs.ChatMessage{{Role: "user", Content: "Count 1 to 5"}},
})
defer stream.Close()
var cost float64
for stream.Next() {
    chunk := stream.Current()
    for _, c := range chunk.Choices { fmt.Print(c.Delta.Content) }
    if chunk.Usage != nil { cost = chunk.Usage.TotalCost }
}
if err := stream.Err(); err != nil { /* … */ }
fmt.Printf("\nCost: $%.6f\n", cost)
```

**Multi-modal** — pass `[]ContentPart` instead of a string:

```go
msg := runjobs.UserMessageParts(
    runjobs.TextPart("What's in this image?"),
    runjobs.ImagePart("https://example.com/photo.jpg", "high"),
)
```

**Tool calling** — populate `Tools` + read `Choices[0].Message.ToolCalls`. Send the tool result back as `runjobs.ToolResultMessage(toolCallID, jsonOutput)`.

**Raw passthrough** — `Chat.NewRaw` / `Chat.NewStreamingRaw` accept a pre-built `json.RawMessage` when you need to forward a body verbatim (proxying agent requests with provider-specific fields).

### `client.Embeddings`

OpenAI-compatible `/v1/embeddings`. Single string or batch.

```go
r, _ := client.Embeddings.Create(ctx, "text-embedding-3-small", runjobs.EmbeddingsParams{
    Input:      []string{"alpha", "beta"},      // string OR []string
    Dimensions: 1536,                            // text-embedding-3-* only
})
vec, _ := r.Data[0].AsFloat32()                  // or AsFloat64()
fmt.Println(r.Usage.TotalCost)
```

`EncodingFormat: "base64"` keeps the vector as a packed base64 blob inside `Embedding.Embedding` (json.RawMessage) — handy for high-D embeddings where the float-array JSON form triples the bytes.

### `client.Image`

```go
img, _ := client.Image.Generate(ctx, "MiniMax Image-01", runjobs.ImageGenerateParams{
    Prompt:             "a gopher painting",
    Size:               "1024x1024",
    N:                  1,
    ReferenceImageURLs: []string{"https://..."},
})

// img.Data[i].URL is either a "data:image/png;base64,..." inline URI (sync)
// or a "https://api.runjobs.ai/v1/blobs/<id>" hosted blob (async). Pass it
// to DecodeMediaURL to get raw bytes:
bytes, mime, _ := runjobs.DecodeMediaURL(ctx, img.Data[0].URL)
fmt.Printf("%d bytes, %s, $%.6f\n", len(bytes), mime, img.Usage.TotalCost)

// Edit (multipart).
f, _ := os.Open("photo.png"); defer f.Close()
edited, _ := client.Image.Edit(ctx, "GPT Image", runjobs.ImageEditParams{
    Image:  f,
    Prompt: "add a party hat",
    // Mask: optional io.Reader for an alpha-mask
})
```

**Async variants** — `Image.GenerateAsync` / `Image.EditAsync` — submit, poll, download. Use them when the request may take longer than ~100 s (large Seedream batches, slow upstream queues); Cloudflare's origin timeout would otherwise replace the real upstream error with a generic `502`. The async methods return the same `*ImageResponse` shape. Bound poll wait via `ctx` deadline (default 10 min).

`ImageResult.Attribution` is a credit string that stock-library providers (Pexels) require you to render verbatim alongside the image.

### `client.Audio` — TTS + STT

**Text-to-speech.** Output bytes land in `SpeechResponse.Data`:

```go
speech, _ := client.Audio.Speech(ctx, "OpenAI/TTS", runjobs.SpeechParams{
    Input: "Hello from the gateway",
    Voice: "nova",
})
os.WriteFile("output.mp3", speech.Data, 0644)
```

The `SpeechParams` cover the full surface — `Voice`, `ResponseFormat`, `Speed`, `Pitch`, `Volume`, `Timber`, `Emotion`, `InstructText` (CosyVoice free-form directive), `ReferenceAudioURL` + `ReferenceText` (zero-shot voice cloning on voiceclone-capable models), and `Extra` for vendor-specific top-level knobs (ACE-Step's `tags`, `duration`, `seed`, etc. — spread at the body root).

```go
// Music generation (ACE-Step).
song, _ := client.Audio.SpeechAsync(ctx, "ACE-Step", runjobs.SpeechParams{
    Input: "[verse]\nUnder the stars tonight",
    Extra: map[string]any{"tags": "indie rock, melancholic", "duration": 60},
})
```

Use `SpeechAsync` whenever generation may exceed the ~100 s sync ceiling (long music, large CosyVoice batches). Same `*SpeechResponse` shape — submit + poll happens internally.

**Speech-to-text.** Multipart upload:

```go
f, _ := os.Open("recording.mp3"); defer f.Close()
t, _ := client.Audio.Transcribe(ctx, "OpenAI/Whisper", runjobs.TranscribeParams{
    File:     f,
    Filename: "recording.mp3",
    Language: "en",                          // optional ISO hint
})
fmt.Println(t.Text)
```

`TranscribeAsync` is the long-audio variant (lectures, multi-hour podcasts). With `ResponseFormat: "verbose_json"` and `TimestampGranularities: []string{"segment"}` or `"word"`, the upstream timing payload lands in `TranscribeResponse.Raw`.

Voice catalogs and supported emotions live on the model's `OptionsSchema().Catalog` (see the Models section). The legacy `Audio.ListVoices` endpoint was removed.

### `client.Video` — async only

```go
task, _ := client.Video.Generate(ctx, "MiniMax Hailuo 2.3", runjobs.VideoGenerateParams{
    Prompt:   "a gentle ocean wave",
    Duration: 5,
})

status, _ := client.Video.Wait(ctx, task.ID)             // polls every 5s; override with WithPollInterval
// or: status, _ := client.Video.GetStatus(ctx, task.ID)  for a single poll

if status.Status == "succeeded" {
    data, mime, _ := client.Video.GetContent(ctx, task.ID)
    fmt.Printf("%s, %d bytes\n", mime, len(data))
}
```

`VideoGenerateParams` covers every gateway field — `AspectRatio`, `Duration` / `Frames`, `Resolution`, `GenerateAudio`, `FirstFrameURL` / `LastFrameURL` (keyframes), `ReferenceImageURLs` / `ReferenceVideoURLs` / `ReferenceAudioURLs` (Seedance 2.0 multi-input), `SourceVideoURL` / `SourceImageURL` / `SourceAudioURL` (single-input drivers for video-edit, motion-transfer, lip-sync), `Watermark`, `CameraFixed`, `ReturnLastFrame`, `Seed`, `Draft` + `DraftTaskID` (Seedance 1.5 pro draft mode), `ServiceTier` ("flex" = offline, ~50 % price), `ExecutionExpiresAfter`, `CallbackURL`, `User`. Tri-state bool fields are pointers so `false` is distinguishable from "unset".

All `*_url` fields accept hosted `https://` URLs or `data:` URIs — use `runjobs.EncodeImageURL(rawBytes)` to wrap local bytes; the gateway materialises data URIs as short-lived blobs before forwarding upstream.

`Video.GenerateRaw(ctx, json.RawMessage)` accepts a pre-built body for verbatim forwarding.

### `client.Computer` — AI GUI control

One step of a computer-use agent loop. Given conversation history (including screenshots and tool results), returns the next action(s) the model wants the caller to execute.

```go
step, _ := client.Computer.Step(ctx, "AI Control", runjobs.ComputerStepParams{
    Messages: []map[string]any{
        {"role": "user", "content": "Open the browser and go to example.com"},
    },
    DisplayWidth:  1920,
    DisplayHeight: 1080,
})

for _, block := range step.Content {
    switch block.Type {
    case "text":          fmt.Println("Text:", block.Text)
    case "tool_use":      fmt.Printf("Action: %s %s\n", block.Name, block.Input) // Anthropic
    case "computer_call": fmt.Printf("Call: %s %v\n", block.CallID, block.Action) // OpenAI
    }
}
```

The `messages` shape is intentionally opaque — both Anthropic and OpenAI computer-use protocols round-trip through it. `step.Protocol` tells you which one the upstream returned; `step.ResponseID` chains state for OpenAI's Responses API (`PreviousResponseID` + `OpenAIInput` on the next step).

### `client.Files` — per-project file system

Backed by `/v1/files/*`. Files are stored under `(user, project)` on the gateway and addressed by POSIX-style paths. Every `FileObject.URL` is a stable, **public** address — embed it in `<img>`, share it, persist it.

Requires an `rrt_*` project-bound token or a hardware-container token whose project instance the gateway can resolve.

```go
// Upload — raw bytes (auto-detects content type when ContentType is empty).
obj, _ := client.Files.Put(ctx, "assets/logo.png", pngBytes, runjobs.PutOptions{
    ContentType: "image/png",
    IfNoneMatch: true,                  // refuse to overwrite (409 on collision)
})
fmt.Println(obj.URL)                    // → https://files.runjobs.ai/userfiles/u/.../p/.../assets/logo.png

// Strings.
client.Files.PutString(ctx, "notes.md", "# hello")

// Server-side ingest — gateway fetches src and stores it (saves a round trip).
client.Files.PutFromURL(ctx, "cache/cat.jpg", "https://example.com/cat.jpg")

// Read.
data, contentType, _ := client.Files.Get(ctx, "assets/logo.png")

// Metadata only (HEAD).
meta, _ := client.Files.Stat(ctx, "assets/logo.png")
fmt.Println(meta.Size, meta.ETag, meta.LastModified)

exists, _ := client.Files.Exists(ctx, "assets/logo.png")

// List — pagination + optional shell-glob filter.
page, _ := client.Files.List(ctx, runjobs.ListOptions{
    Prefix: "assets/",                  // optional namespace
    Glob:   "**/*.png",                 // *, **, ?, [abc]
    Limit:  100,
    Cursor: "",                         // page.NextCursor for the next page
})
for _, f := range page.Files { fmt.Println(f.Path, f.Size) }

// Mutate.
client.Files.Move(ctx, "draft.md", "published.md")
client.Files.Copy(ctx, "template.md", "instances/2026-05-16.md")
client.Files.Delete(ctx, "old.log")                                // idempotent

// Bulk delete by prefix / glob (at least one required).
n, _ := client.Files.DeleteMany(ctx, "tmp/", "")                   // wipe a "directory"
n, _ = client.Files.DeleteMany(ctx, "", "**/*.tmp")                // wipe by pattern
n, _ = client.Files.DeleteMany(ctx, "logs/", "*.bak")

// Pipeline several ops in one round trip. Operations execute in submission
// order; one op failing does NOT abort the rest — inspect each Result.OK.
// Auto-chunks long slices into 30-op requests (gateway caps at 64).
results, _ := client.Files.Batch(ctx, []runjobs.BatchOp{
    {Op: "put_url", Path: "a.jpg", SrcURL: "https://example.com/a.jpg"},
    {Op: "copy",    From: "a.jpg", To: "b.jpg"},
    {Op: "exists",  Path: "b.jpg"},
    {Op: "stat",    Path: "b.jpg"},
    {Op: "del",     Path: "a.jpg"},
})
```

`Glob` patterns are matched after the underlying S3 prefix scan. The gateway folds the literal head of the pattern into the prefix automatically — you don't have to think about it. Patterns match the WHOLE path; no implicit anchoring.

## Helpers

**`runjobs.EncodeImageURL(raw []byte) string`** — wrap raw bytes as `data:<mime>;base64,…` for any `*_url` field that accepts data URIs (`FirstFrameURL`, `LastFrameURL`, `ReferenceImageURLs`, `SourceImageURL`, …). MIME is sniffed via `http.DetectContentType`.

**`runjobs.DecodeMediaURL(ctx, url) ([]byte, mime, error)`** — inverse: resolves the `data:` URI or hosted `https://` URL into raw bytes + MIME. Used on every image / audio result. 60 s default HTTP timeout; override via `ctx` deadline.

## Error Handling

Every non-2xx response surfaces as `*runjobs.APIError`:

```go
var apiErr *runjobs.APIError
if errors.As(err, &apiErr) {
    fmt.Println(apiErr.StatusCode, apiErr.Type, apiErr.Message)
}
```

The SDK extracts the human-readable message even when the upstream wraps it in OpenAI-style `{"error":{"message":…}}` or doubly-nested envelopes. Non-JSON error bodies (HTML 502 pages from intermediaries) are surfaced raw, truncated at 2 KiB.

Network errors (DNS, socket reset, ctx cancel) propagate as the underlying transport error — wrap them yourself if you need retry logic.

## API Reference

### Services

| Service              | Methods | Description |
|----------------------|---------|-------------|
| `client.Chat`        | `New`, `NewStreaming`, `NewRaw`, `NewStreamingRaw` | OpenAI-compatible chat completions |
| `client.Models`      | `List` (with `WithCapability(...)`) | Model catalog + pricing + capability tags + options schema |
| `client.Embeddings`  | `Create` | OpenAI-compatible `/v1/embeddings` |
| `client.Image`       | `Generate`, `Edit`, `GenerateAsync`, `EditAsync` | Image generation + editing. Async variants for >100 s jobs |
| `client.Audio`       | `Speech`, `SpeechAsync`, `Transcribe`, `TranscribeAsync`, `SpeechRaw` | TTS + STT. Async variants for long music / multi-hour audio |
| `client.Video`       | `Generate`, `GenerateRaw`, `GetStatus`, `Wait` (`WithPollInterval`), `GetContent` | Async video generation |
| `client.Computer`    | `Step` | AI GUI control loop (Anthropic + OpenAI protocols) |
| `client.Files`       | `Put`, `PutString`, `PutFromURL`, `Get`, `Stat`, `Exists`, `Delete`, `List`, `DeleteMany`, `Move`, `Copy`, `Batch` | Per-project file system at `/v1/files/*` |

### `Model` helpers

| Method | Returns | Use |
|--------|---------|-----|
| `m.HasCapabilityTag(id)` | `bool` | Filter by stable capability tag |
| `m.CapabilityTags` | `[]Tag` | Iterate `{ID, Label}` for display chips |
| `m.OptionsSchema()` | `*Schema, error` | Typed view of the model's input contract |
| `m.AcceptsField(name)` | `bool` | Does the model accept this field? |
| `m.RequiresField(name)` | `bool` | Is this field required? |
| `m.AllowedValuesFor(name)` | `[]any` | Discrete enum, or nil |
| `m.AvailableVoices` | `[]string` | TTS voice IDs (top-level convenience) |
| `m.SupportsVoiceClone()` | `bool` | Accepts `ReferenceAudioURL` as the voice? |
| `m.SupportsInstructText()` | `bool` | Accepts `InstructText` directive? |
| `m.DefaultVoice()` | `string` | Admin-configured default voice |

### `Schema` helpers

| Method | Returns | Use |
|--------|---------|-----|
| `schema.ValidateRequest(req)` | `ValidationErrors` | Pre-flight validate a request body against the schema |

### Media helpers

| Function | Use |
|----------|-----|
| `runjobs.EncodeImageURL(bytes)` | Wrap raw bytes as a `data:` URI for any `*_url` field |
| `runjobs.DecodeMediaURL(ctx, url)` | Resolve a `data:` URI or hosted URL into bytes + MIME |

## License

MIT
