// Package runjobs provides a Go client for the RunJobs AI Gateway.
//
// It composes the [openai-go] SDK for chat completions and extends it with
// gateway-specific services: model catalog with pricing, image generation
// and editing, text-to-speech, speech-to-text, async video generation,
// and computer use (AI GUI control).
//
// # Quick Start
//
//	client := runjobs.NewClient("gw-your-api-key",
//	    runjobs.WithBaseURL("https://api.runjobs.ai"),
//	)
//
//	// Chat — uses openai-go directly
//	resp, err := client.Chat.New(ctx, openai.ChatCompletionNewParams{
//	    Model:    "Claude Haiku 4.5",
//	    Messages: []openai.ChatCompletionMessageParamUnion{
//	        openai.UserMessage("Hello!"),
//	    },
//	})
//
//	// Gateway-specific services
//	models, _ := client.Models.List(ctx, runjobs.WithCapability("text"))
//	img, _    := client.Image.Generate(ctx, "MiniMax Image-01", runjobs.ImageGenerateParams{...})
//	speech, _ := client.Audio.Speech(ctx, "OpenAI/TTS", runjobs.SpeechParams{...})
//	task, _   := client.Video.Generate(ctx, "MiniMax Hailuo 2.3", runjobs.VideoGenerateParams{...})
//	step, _   := client.Computer.Step(ctx, "AI Control", runjobs.ComputerStepParams{...})
//
// # Error Handling
//
// All errors — including gateway-specific endpoints — are returned as
// [*openai.Error], so callers can use a single [errors.As] pattern:
//
//	var apiErr *openai.Error
//	if errors.As(err, &apiErr) {
//	    fmt.Println(apiErr.StatusCode, apiErr.Message)
//	}
//
// [openai-go]: https://github.com/openai/openai-go
package runjobs
