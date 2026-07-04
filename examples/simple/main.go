package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/provider"
	"github.com/B777B2056-2/kugelblitz/runtime"
)

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY not set")
	}

	p := provider.DeepSeek(apiKey, "https://api.deepseek.com", "deepseek-chat")
	cfg := config.Config{
		Model: config.ModelConfig{
			Provider:        p,
			StreamMode:      true,
			EnableThinking:  true,
			ReasoningEffort: core.ReasoningEffortHigh,
		},
		Runtime:         config.RuntimeConfig{MaxStateMachineCycles: 30},
		ContextCompress: config.ContextCompressConfig{MaxAttempts: 1, MaxToolResultChars: 4000},
		TargetDrift:     config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
	}
	loop := runtime.NewAgentLoop(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		<-loop.Done()
		cancel()
	}()

	goal := "List the files in the current directory and summarize what the project is about"
	loop.Run(ctx, goal)

	<-ctx.Done()
	log.Println("done")
}
