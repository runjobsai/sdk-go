# RunJobs SDK for Go

Go client for the [RunJobs AI Gateway](https://github.com/runjobsai/ai-gateway). Zero external dependencies.

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

## Services

### Models

List available models with pricing, capability tags, and the
structured input schema.

```go
models, _ := client.Models.List(ctx)

// Filter by capability
videoModels, _ := client.Models.List(ctx, runjobs.WithCapability("video_generation"))

for _, m := range videoModels {
    // CapabilityTags is the auto-derived "what this model can actually
    // do" array — much more informative than the broad capability bucket.
    var tagIDs []string
    for _, t := range m.CapabilityTags {
        tagIDs = append(tagIDs, t.ID)
    }
    fmt.Printf("%-30s [%s]  in=%d out=%d pips/MTok\n",
        m.ID, strings.Join(tagIDs, ","),
        m.InputPricePerMTok, m.OutputPricePerMTok)
    // → "Seedance 2.0  [t2v,first_last_frame,reference,audio_track]  in=0 out=0 pips/MTok"
}

// HasCapabilityTag for quick filter checks (use stable IDs, not labels):
for _, m := range videoModels {
    if m.HasCapabilityTag("first_last_frame") {
        fmt.Println(m.ID, "supports first/last keyframe input")
    }
}
```

Tag vocabulary (per capability):

| Capability         | Stable IDs |
|--------------------|------------|
| `video_generation` | `t2v`, `i2v`, `v2v`, `a2v`, `first_last_frame`, `reference`, `motion_transfer`, `audio_track` |
| `image_generation` | `t2i`, `i2i`, `inpaint` |
| `text_to_speech`   | `tts`, `voice_clone`, `instruct`, `emotion`, `voice_catalog` |
| `speech_to_text`   | `stt`, `timestamps` |
| `embedding`        | `embedding` |
| `text` / `vision`  | `chat`, `vision` |
| `computer_use`     | `computer_use` |

The `Tag.Label` field is a human-readable English string (`"Image-to-Video"`,
`"First/Last Frame"`, …) — translate / re-style on your side. **Filter on
`Tag.ID`**; labels can shift between gateway versions.

### Model Capabilities & Validation (Options Schema)

Each model row carries a typed `Schema` describing exactly which
request fields it accepts, with bounds, enums, defaults, roles, and
cross-field constraints. Use it to build dynamic UIs and to validate
request bodies *before* shipping them to the gateway.

```go
list, _ := client.Models.List(ctx, runjobs.WithCapability("text_to_speech"))
var cosy runjobs.Model
for _, m := range list {
    if m.ID == "CosyVoice" { cosy = m }
}

schema, _ := cosy.OptionsSchema() // typed *runjobs.Schema, nil if no options

// Quick presence checks — handy for "should I render this UI chip" decisions.
if cosy.AcceptsField("reference_audio_url") { /* show voice-clone uploader */ }
if cosy.RequiresField("source_audio_url")   { /* mark this field with a red star */ }
allowed := cosy.AllowedValuesFor("emotion")  // []any{"happy","sad",...} or nil

// Voice catalog (id + name + gender + language + preview_url, ...) lives
// on the schema — not on the legacy options map.
if schema != nil && schema.Catalog != nil {
    for _, v := range schema.Catalog.Voices {
        fmt.Printf("%s  %s  %s\n", v["id"], v["name"], v["gender"])
    }
    fmt.Println("Emotions:", schema.Catalog.Emotions)
}

// Or if you only need the IDs (no metadata), the gateway also exposes
// them as a top-level convenience field.
fmt.Println("Voice IDs:", cosy.AvailableVoices)

// Pre-flight validate a request body before sending — catches missing
// required fields, out-of-range numbers, mutually-exclusive combos,
// pixel-bounds violations, etc. Returns ValidationErrors (slice).
errs := schema.ValidateRequest(map[string]any{
    "input": "Hello",
    "voice": "alloy",
    "reference_audio_url": "https://x/sample.wav", // mutex violation: voice XOR reference
})
for _, e := range errs {
    fmt.Printf("  %s: %s\n", e.Field, e.Reason)
}
// → "voice/reference_audio_url: at most one of voice, reference_audio_url may be set"
```

Constraint vocabulary the validator understands: `any_of_required`,
`mutually_exclusive`, `group_mutex` (block-level XOR — e.g.
Seedance/Veo "keyframe block XOR reference block"), `requires_all`
(when X is set, Y must also be set — e.g. `last_frame_url` requires
`first_frame_url`), `pixel_bounds`. Unknown constraint kinds are
silently skipped (forward-compat with newer gateway versions).

### Chat

Supports both streaming and non-streaming, using OpenAI-compatible format.

```go
// Non-streaming
resp, _ := client.Chat.New(ctx, runjobs.ChatCompletionParams{
    Model: "Claude Sonnet 4.6",
    Messages: []runjobs.ChatMessage{
        {Role: "user", Content: "Explain Go interfaces in one sentence."},
    },
})
fmt.Println(resp.Choices[0].Message.Content)
fmt.Printf("Cost: $%.6f\n", resp.Usage.TotalCost)

// Streaming
stream := client.Chat.NewStreaming(ctx, runjobs.ChatCompletionParams{
    Model: "Gemini 3 Flash",
    Messages: []runjobs.ChatMessage{
        {Role: "user", Content: "Count 1 to 5"},
    },
})
defer stream.Close()
var cost float64
for stream.Next() {
    chunk := stream.Current()
    for _, c := range chunk.Choices {
        fmt.Print(c.Delta.Content)
    }
    if chunk.Usage != nil {
        cost = chunk.Usage.TotalCost
    }
}
fmt.Printf("\nCost: $%.6f\n", cost)
```

### Image Generation & Editing

```go
// Generate
img, _ := client.Image.Generate(ctx, "MiniMax Image-01", runjobs.ImageGenerateParams{
    Prompt: "a gopher painting",
    Size:   "1024x1024",
})
// img.Data[0].URL is either "data:image/png;base64,..." (sync) or
// "https://api.runjobs.ai/v1/blobs/<id>" (async). DecodeMediaURL
// resolves either shape into raw bytes + mime.
bytes, mime, _ := runjobs.DecodeMediaURL(ctx, img.Data[0].URL)
fmt.Printf("Got %d bytes (%s)\n", len(bytes), mime)
fmt.Printf("Cost: $%.6f\n", img.Usage.TotalCost)

// Edit (multipart)
f, _ := os.Open("photo.png")
defer f.Close()
edited, _ := client.Image.Edit(ctx, "GPT Image", runjobs.ImageEditParams{
    Image:  f,
    Prompt: "add a party hat",
})
fmt.Printf("Cost: $%.6f\n", edited.Usage.TotalCost)
```

`Image.Generate` and `Image.Edit` hit the gateway's synchronous OpenAI-compatible endpoints (`POST /v1/images/generations`, `POST /v1/images/edits`). For requests expected to run longer than ~100 seconds — large Seedream batches, slow upstream queues — use `Image.GenerateAsync` / `Image.EditAsync` instead. The Async variants submit the job, poll the gateway for completion, and download the result blobs. They return the same `*ImageResponse` shape as the sync methods but avoid Cloudflare's origin timeout (which otherwise replaces the real upstream error with `error code: 502`). The caller's `ctx` deadline bounds the poll wait (default 10 min).

### Text-to-Speech & Speech-to-Text

```go
// Voice catalog (id + name + gender + language + preview_url + …)
// lives on the model's options Schema — see "Model Capabilities &
// Validation" above for the full pattern. Quick lookup:
list, _ := client.Models.List(ctx, runjobs.WithCapability("text_to_speech"))
var minimax runjobs.Model
for _, m := range list {
    if m.ID == "MiniMax Speech 2.6 HD" {
        minimax = m
    }
}
schema, _ := minimax.OptionsSchema()
if schema != nil && schema.Catalog != nil {
    for _, v := range schema.Catalog.Voices {
        fmt.Printf("%s  %s  %s  %s\n", v["id"], v["name"], v["gender"], v["language"])
    }
    fmt.Println("Emotions:", schema.Catalog.Emotions)
    // → ["happy","sad","angry","fearful","disgusted","surprised","calm","whisper"]
}

// TTS (basic)
speech, _ := client.Audio.Speech(ctx, "OpenAI/TTS", runjobs.SpeechParams{
    Input: "Hello from the gateway",
    Voice: "nova",
})
os.WriteFile("output.mp3", speech.Data, 0644)
fmt.Printf("Cost: $%.6f\n", speech.Usage.TotalCost)

// TTS with emotion/speed (optional, provider-dependent)
speech, _ = client.Audio.Speech(ctx, "MiniMax Speech 2.6 HD", runjobs.SpeechParams{
    Input:   "I'm so happy to see you!",
    Voice:   "English_radiant_girl",
    Speed:   1.1,
    Emotion: "happy", // optional; omit to let the model auto-detect
})

// STT
audio, _ := os.Open("recording.mp3")
defer audio.Close()
transcript, _ := client.Audio.Transcribe(ctx, "OpenAI/Whisper", runjobs.TranscribeParams{
    File:     audio,
    Filename: "recording.mp3",
})
fmt.Println(transcript.Text)
fmt.Printf("Cost: $%.6f\n", transcript.Usage.TotalCost)
```

### Video Generation (Async)

```go
// Submit
task, _ := client.Video.Generate(ctx, "MiniMax Hailuo 2.3", runjobs.VideoGenerateParams{
    Prompt:   "a gentle ocean wave",
    Duration: 5,
})
fmt.Println("Task:", task.ID, "Cost:", task.Usage.TotalCost)

// Wait for completion (polls every 5s by default)
status, _ := client.Video.Wait(ctx, task.ID)

// Download
if status.Status == "succeeded" {
    data, mime, _ := client.Video.GetContent(ctx, task.ID)
    fmt.Printf("%s, %d bytes\n", mime, len(data))
}
```

### Computer Use (AI GUI Control)

```go
step, _ := client.Computer.Step(ctx, "AI Control", runjobs.ComputerStepParams{
    Messages: []map[string]any{
        {
            "role": "user",
            "content": "Open the browser and go to example.com",
        },
    },
    DisplayWidth:  1920,
    DisplayHeight: 1080,
})

for _, block := range step.Content {
    switch block.Type {
    case "text":
        fmt.Println("Text:", block.Text)
    case "tool_use":
        fmt.Printf("Action: %s %v\n", block.Name, block.Input)
    case "computer_call":
        fmt.Printf("Call: %s %v\n", block.CallID, block.Action)
    }
}
fmt.Printf("Cost: $%.6f\n", step.Usage.TotalCost)
```

## Error Handling

All errors use `*runjobs.APIError`:

```go
var apiErr *runjobs.APIError
if errors.As(err, &apiErr) {
    fmt.Println(apiErr.StatusCode, apiErr.Message)
}
```

## API Reference

### Services

| Service | Methods | Description |
|---------|---------|-------------|
| `client.Chat` | `New`, `NewStreaming` | OpenAI-compatible chat completions |
| `client.Models` | `List` | Model catalog with pricing, capability tags, and options schema |
| `client.Image` | `Generate`, `Edit`, `GenerateAsync`, `EditAsync` | Image generation and editing |
| `client.Audio` | `Speech`, `Transcribe` | Text-to-speech and speech-to-text (voice catalog on the model's `OptionsSchema().Catalog`) |
| `client.Video` | `Generate`, `GetStatus`, `Wait`, `GetContent` | Async video generation |
| `client.Computer` | `Step` | Computer use (AI GUI control) |

### Model helpers

Methods on the `Model` value returned by `Models.List`:

| Method | Returns | Use for |
|--------|---------|---------|
| `m.HasCapabilityTag(id)` | `bool` | Filter models by stable capability tag (`"i2v"`, `"voice_clone"`, …). |
| `m.CapabilityTags` | `[]Tag` | Iterate `{ID, Label}` for display chips. |
| `m.OptionsSchema()` | `*Schema, error` | Typed view of the model's input contract — Inputs, Constraints, Catalog. |
| `m.AcceptsField(name)` | `bool` | Does the model accept this request field at all? |
| `m.RequiresField(name)` | `bool` | Is this field required (red-star UI)? |
| `m.AllowedValuesFor(name)` | `[]any` | Discrete enum for dropdown options, or nil. |
| `m.AvailableVoices` | `[]string` | TTS-only: voice IDs the model accepts (top-level convenience field). |

| Schema method | Returns | Use for |
|---------------|---------|---------|
| `schema.ValidateRequest(req)` | `ValidationErrors` | Pre-flight validate a request body against the schema before submitting. |

## License

MIT
