// Example: ACP (Agent Client Protocol) Server.
//
// This starts a Kugelblitz agent as an ACP-compatible server over stdin/stdout.
// Any ACP-compatible editor (Zed, JetBrains, VS Code with extension, Neovim)
// can use this as its AI coding agent backend.
//
// Usage:
//
//	go run . -provider deepseek -apikey sk-xxx
//	go run . -provider openai -apikey sk-xxx -model gpt-4.1-mini
//	go run . -apikey sk-xxx -stream=false
//
// Editor configuration (example for Zed):
//
//	{
//	  "agent": {
//	    "command": "go",
//	    "args": ["run", ".", "-apikey", "sk-xxx"],
//	    "work_dir": "/path/to/kugelblitz/examples/acp_server"
//	  }
//	}
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/B777B2056-2/kugelblitz/acp"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/provider"
	"github.com/B777B2056-2/kugelblitz/runtime"

	_ "github.com/B777B2056-2/kugelblitz/tools/internals" // auto-register built-in tools
)

func main() {
	// ---- Parse command-line flags ----
	providerChoice := flag.String("provider", "deepseek", "LLM provider: deepseek | openai")
	apiKey := flag.String("apikey", "", "API key for the provider (required)")
	model := flag.String("model", "deepseek-v4-flash", "Model name")
	baseURL := flag.String("baseurl", "https://api.deepseek.com", "Custom API base URL")
	enableThinking := flag.Bool("thinking", true, "Enable thinking/reasoning mode")
	streamMode := flag.Bool("stream", true, "Streaming mode")
	verbose := flag.Bool("v", false, "Verbose logging to stderr")
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if *apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: API key required (-apikey flag or DEEPSEEK_API_KEY env)\n")
		os.Exit(1)
	}

	// ---- Create provider ----
	p := createProvider(*providerChoice, *apiKey, *baseURL, *model)

	// ---- Create agent ----
	agent := runtime.NewReactAgent(p, *streamMode)
	if *enableThinking {
		agent.SetThinking(true, core.ReasoningEffortHigh)
	}

	// ---- Configure logging ----
	if !*verbose {
		core.SetLogger(core.DiscardLogger())
	}

	// ---- Create ACP server ----
	srv := acp.NewServer(agent, p)

	// ---- Signal handling ----
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// ---- Run ----
	core.Info("starting ACP server", "provider", *providerChoice, "model", *model, "stream", *streamMode)

	if err := srv.Run(ctx); err != nil {
		core.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func createProvider(name, apiKey, baseURL, model string) core.ILMProvider {
	switch name {
	case "openai":
		return provider.OpenAI(apiKey, baseURL, model)
	default:
		return provider.DeepSeek(apiKey, baseURL, model)
	}
}
