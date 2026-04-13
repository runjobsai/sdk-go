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

List available models with pricing and capability metadata.

```go
models, _ := client.Models.List(ctx)

// Filter by capability
textModels, _ := client.Models.List(ctx, runjobs.WithCapability("text"))

for _, m := range textModels {
    fmt.Printf("%s  in=%d out=%d pips/MTok\n",
        m.ID, m.InputPricePerMTok, m.OutputPricePerMTok)
}
```

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
fmt.Println(len(img.Data[0].B64JSON)) // base64 image data
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

`Image.Generate` and `Image.Edit` use the gateway's async job protocol internally: the client submits the job, polls every 2 s for completion, and downloads the resulting blob URLs. All polling is hidden — callers see the same `*ImageResponse` shape regardless of job duration, and the caller's `ctx` deadline (default 10 min) bounds the wait.

### Text-to-Speech & Speech-to-Text

```go
// List voices and supported emotions for a TTS model
catalog, _ := client.Audio.ListVoices(ctx, "MiniMax Speech 2.6 HD")
for _, v := range catalog.Voices {
    fmt.Printf("%s  %s  %s  %s\n", v.ID, v.Name, v.Gender, v.Language)
}
fmt.Println("Emotions:", catalog.SupportedEmotions)
// e.g. ["happy", "sad", "angry", "fearful", "disgusted", "surprised", "calm", "whisper"]

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

| Service | Methods | Description |
|---------|---------|-------------|
| `client.Chat` | `New`, `NewStreaming` | OpenAI-compatible chat completions |
| `client.Models` | `List` | Model catalog with pricing and capabilities |
| `client.Image` | `Generate`, `Edit` | Image generation and editing |
| `client.Audio` | `ListVoices`, `Speech`, `Transcribe` | Voice catalog, text-to-speech, and speech-to-text |
| `client.Video` | `Generate`, `GetStatus`, `Wait`, `GetContent` | Async video generation |
| `client.Computer` | `Step` | Computer use (AI GUI control) |

## License

MIT
