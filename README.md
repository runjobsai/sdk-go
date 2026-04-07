# RunJobs SDK for Go

Go client for the [RunJobs AI Gateway](https://github.com/runjobsai/ai-gateway). Extends [openai-go](https://github.com/openai/openai-go) with gateway-specific services.

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

    "github.com/openai/openai-go/v3"
    runjobs "github.com/runjobsai/sdk-go"
)

func main() {
    client := runjobs.NewClient("gw-your-api-key",
        runjobs.WithBaseURL("https://api.runjobs.ai"),
    )
    ctx := context.Background()

    // Chat (openai-go native — streaming also available via NewStreaming)
    resp, err := client.Chat.New(ctx, openai.ChatCompletionNewParams{
        Model: "Claude Haiku 4.5",
        Messages: []openai.ChatCompletionMessageParamUnion{
            openai.UserMessage("Hello!"),
        },
    })
    if err != nil {
        var apiErr *openai.Error
        if errors.As(err, &apiErr) {
            fmt.Printf("API error %d: %s\n", apiErr.StatusCode, apiErr.Message)
        }
        return
    }
    fmt.Println(resp.Choices[0].Message.Content)
}
```

## Services

### Models

List available models with pricing and capability metadata.

```go
// All models
models, _ := client.Models.List(ctx)

// Filter by capability
textModels, _ := client.Models.List(ctx, runjobs.WithCapability("text"))

for _, m := range textModels {
    fmt.Printf("%s  in=%d out=%d pips/MTok\n",
        m.ID, m.InputPricePerMTok, m.OutputPricePerMTok)
}
```

### Chat

Direct passthrough to `openai-go` — supports both streaming and non-streaming.

```go
// Non-streaming
resp, _ := client.Chat.New(ctx, openai.ChatCompletionNewParams{
    Model:    "Claude Sonnet 4.6",
    Messages: []openai.ChatCompletionMessageParamUnion{
        openai.UserMessage("Explain Go interfaces in one sentence."),
    },
})

// Streaming
stream := client.Chat.NewStreaming(ctx, openai.ChatCompletionNewParams{
    Model:    "Gemini 3 Flash",
    Messages: []openai.ChatCompletionMessageParamUnion{
        openai.UserMessage("Count 1 to 5"),
    },
})
for stream.Next() {
    chunk := stream.Current()
    for _, c := range chunk.Choices {
        fmt.Print(c.Delta.Content)
    }
}
```

### Image Generation & Editing

```go
// Generate
img, _ := client.Image.Generate(ctx, "MiniMax Image-01", runjobs.ImageGenerateParams{
    Prompt: "a gopher painting",
    Size:   "1024x1024",
})
fmt.Println(len(img.Data[0].B64JSON)) // base64 image data

// Edit (multipart)
f, _ := os.Open("photo.png")
defer f.Close()
edited, _ := client.Image.Edit(ctx, "GPT Image", runjobs.ImageEditParams{
    Image:  f,
    Prompt: "add a party hat",
})
```

### Text-to-Speech & Speech-to-Text

```go
// TTS
speech, _ := client.Audio.Speech(ctx, "OpenAI/TTS", runjobs.SpeechParams{
    Input: "Hello from the gateway",
    Voice: "nova",
})
os.WriteFile("output.mp3", speech.Data, 0644)

// STT
audio, _ := os.Open("recording.mp3")
defer audio.Close()
transcript, _ := client.Audio.Transcribe(ctx, "OpenAI/Whisper", runjobs.TranscribeParams{
    File:     audio,
    Filename: "recording.mp3",
})
fmt.Println(transcript.Text)
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
// Execute one step of a computer-use agent loop
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

// Inspect the model's actions
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
```

## Error Handling

All errors — including gateway-specific endpoints — use `*openai.Error`:

```go
var apiErr *openai.Error
if errors.As(err, &apiErr) {
    fmt.Println(apiErr.StatusCode, apiErr.Message)
}
```

## API Reference

| Service | Methods | Description |
|---------|---------|-------------|
| `client.Chat` | `New`, `NewStreaming` | OpenAI-compatible chat (via openai-go) |
| `client.Models` | `List` | Model catalog with pricing and capabilities |
| `client.Image` | `Generate`, `Edit` | Image generation and editing |
| `client.Audio` | `Speech`, `Transcribe` | Text-to-speech and speech-to-text |
| `client.Video` | `Generate`, `GetStatus`, `Wait`, `GetContent` | Async video generation |
| `client.Computer` | `Step` | Computer use (AI GUI control) |

## License

MIT
