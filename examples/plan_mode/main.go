// Example: Planner agent with plan tools + worker_spawn.
// Supports optional Langfuse observability.
//
// Usage:
//
//	go run . -apikey sk-xxx
//	go run . -apikey sk-xxx -model deepseek-chat -thinking=false
//	go run . -apikey sk-xxx -stream=false
//	go run . -apikey sk-xxx -langfuse-host http://localhost:3000 -langfuse-pk pk-xxx -langfuse-sk sk-xxx
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"kugelblitz/core"
	"kugelblitz/observability"
	"kugelblitz/provider"
	"kugelblitz/runtime"
)

func main() {
	// ---- 1. Parse flags ----
	apiKey := flag.String("apikey", "", "API key (required)")
	model := flag.String("model", "deepseek-v4-flash", "Model name")
	enableThinking := flag.Bool("thinking", true, "Enable thinking mode")
	streamMode := flag.Bool("stream", true, "Streaming output")

	// Langfuse flags (all optional — observability is disabled unless all three are provided)
	lfHost := flag.String("langfuse-host", "http://localhost:3000", "Langfuse host (e.g. http://localhost:3000)")
	lfPK := flag.String("langfuse-pk", "", "Langfuse public key")
	lfSK := flag.String("langfuse-sk", "", "Langfuse secret key")
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintf(os.Stderr, "Usage: go run . -apikey <key> [flags]\n")
		fmt.Fprintf(os.Stderr, "  -langfuse-host  Langfuse host URL\n")
		fmt.Fprintf(os.Stderr, "  -langfuse-pk    Langfuse public key\n")
		fmt.Fprintf(os.Stderr, "  -langfuse-sk    Langfuse secret key\n")
		os.Exit(1)
	}

	// ---- 2. Create provider ----
	p := provider.DeepSeek(*apiKey, "https://api.deepseek.com", *model)

	// ---- 3. Observability (optional) ----
	var opts []runtime.PlannerOption
	var lfObs *observability.LangfuseObserver

	if *lfHost != "" && *lfPK != "" && *lfSK != "" {
		lfObs = observability.NewLangfuseObserver(observability.LangfuseConfig{
			Host:      *lfHost,
			PublicKey: *lfPK,
			SecretKey: *lfSK,
		})
		opts = append(opts, runtime.WithObserver(lfObs))
		fmt.Printf("Langfuse: %s\n", *lfHost)
	} else {
		fmt.Println("Langfuse: disabled (pass -langfuse-host, -langfuse-pk, -langfuse-sk to enable)")
	}

	// ---- 4. Create Planner ----
	planner := runtime.NewPlanner(p, *streamMode, opts...)
	if *enableThinking {
		planner.SetThinking(true, core.ReasoningEffortHigh)
	}
	planner.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: &consoleHandler{},
		OnToolCallEnd: func(result core.ToolCallResult) {
			fmt.Printf("\n┌─ [%s] ─────────────────────────────\n", result.ToolName)
			for k, v := range result.Outputs {
				s := fmt.Sprint(v)
				if len(s) > 200 {
					s = s[:200] + "..."
				}
				fmt.Printf("│  %s: %s\n", k, s)
			}
			fmt.Printf("└──────────────────────────────────────\n")
		},
	})

	// ---- 5. Run ----
	ctx := context.Background()
	goal := "创建一个项目说明文件 README.md，内容为当前项目概述；然后创建 docs 目录并在其中放一个 architecture.md，描述项目架构"

	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("Goal: %s\n", goal)
	fmt.Println("───────────────────────────────────────────")

	messages, err := planner.Execute(ctx, goal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	for _, msg := range messages {
		if tc, ok := msg.Content.(core.TextContent); ok {
			fmt.Println(tc.Text)
		}
	}
	fmt.Println("\n═══════════════════════════════════════════")

	// ---- 6. Flush Langfuse ----
	if lfObs != nil {
		fmt.Println("Flushing traces to Langfuse...")
		if err := lfObs.Flush(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "langfuse flush: %v\n", err)
		}
	}
}

// consoleHandler prints streaming chunks in real time.
type consoleHandler struct{}

func (h *consoleHandler) OnThinkingChunk(chunk string) { fmt.Print(chunk) }
func (h *consoleHandler) OnReplyChunk(chunk string)    { fmt.Print(chunk) }
func (h *consoleHandler) OnFunctionCall(d core.ToolCallDetail) {
	fmt.Printf("\n┌─ [call] %s\n", d.ToolName)
	for k, v := range d.Args {
		fmt.Printf("│  %s: %v\n", k, v)
	}
	fmt.Printf("└──────────────────────────────────────\n")
}
func (h *consoleHandler) OnFinished(reason string) {
	fmt.Printf("\n[finished: %s]\n", reason)
}
func (h *consoleHandler) OnUsageUpdated(usage core.Usage) {
	fmt.Printf("[usage] in=%d out=%d reasoning=%d total=%d\n",
		usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens, usage.TotalTokens)
}
func (h *consoleHandler) OnError(err error) {
	if err != nil && !strings.Contains(err.Error(), "stream") {
		fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
	}
}
