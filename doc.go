// Package runjobs provides a Go client for the RunJobs AI Gateway.
//
// It provides services for chat completions (OpenAI-compatible), model catalog
// with pricing, image generation and editing, text-to-speech, speech-to-text,
// async video generation, and computer use (AI GUI control).
//
// # Quick Start
//
//	client := runjobs.NewClient("gw-your-api-key",
//	    runjobs.WithBaseURL("https://api.runjobs.ai"),
//	)
//
//	// Chat
//	resp, err := client.Chat.New(ctx, runjobs.ChatCompletionParams{
//	    Model: "Claude Haiku 4.5",
//	    Messages: []runjobs.ChatMessage{
//	        {Role: "user", Content: "Hello!"},
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
// All errors from the gateway are returned as [*APIError]:
//
//	var apiErr *runjobs.APIError
//	if errors.As(err, &apiErr) {
//	    fmt.Println(apiErr.StatusCode, apiErr.Message)
//	}
package runjobs
