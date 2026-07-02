// Example: ReAct Agent with tool calling and streaming.
//
// Usage:
//
//	go run . -provider deepseek -apikey sk-xxx
//	go run . -apikey sk-xxx                                           # 默认配置
//	go run . -apikey sk-xxx -provider openai -model gpt-4.1-mini     # OpenAI
//	go run . -apikey sk-xxx -thinking=false                           # 关闭思考
//	go run . -apikey sk-xxx -stream=false                             # 非流式
//	go run . -apikey sk-xxx -baseurl https://api.my-proxy.com        # 自定义端点
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/provider"
	"github.com/B777B2056-2/kugelblitz/runtime"

	_ "github.com/B777B2056-2/kugelblitz/tools/internals" // auto-register built-in tools
)

func main() {
	// ---- 1. Parse command-line flags ----
	providerChoice := flag.String("provider", "deepseek", "LLM provider: deepseek | openai")
	apiKey := flag.String("apikey", "", "API key for the provider (required)")
	model := flag.String("model", "deepseek-v4-flash", "Model name")
	baseURL := flag.String("baseurl", "https://api.deepseek.com", "Custom API base URL (default: provider's official endpoint)")
	enableThinking := flag.Bool("thinking", true, "Enable thinking/reasoning mode (DeepSeek)")
	streamMode := flag.Bool("stream", true, "Streaming mode: true=real-time output, false=wait for full response")
	flag.Parse()

	if *apiKey == "" {
		printUsageAndExit()
	}

	// ---- 2. Create provider ----
	p := createProvider(*providerChoice, *apiKey, *baseURL, *model, *enableThinking, *streamMode)

	// ---- 3. Register tools ----
	registerTools()

	// ---- 4. Create agent & configure tool visibility ----
	agent := runtime.NewReactAgent(p, *streamMode)
	if *enableThinking {
		agent.SetThinking(true, core.ReasoningEffortHigh)
	}
	// Restrict to specific tools — agents only see what they need
	agent.WithTools("get_current_time", "calculate")

	// ---- 5. Register event hooks ----
	agent.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: &consoleEventHandler{},
		OnToolCallEnd: func(result core.ToolCallResult) {
			fmt.Printf("\n┌─ [tool] %s ─────────────────────────────\n", result.ToolName)
			for k, v := range result.Outputs {
				fmt.Printf("│  %s: %v\n", k, v)
			}
			fmt.Printf("└──────────────────────────────────────────\n")
		},
	})

	// ---- 6. Run conversation ----
	ctx := context.Background()

	systemMsg := core.NewSystemMessage("root", core.TextContent{
		Text: "You are a helpful assistant. Use tools when helpful, then answer in the user's language.",
	})

	userMsg := core.NewUserMessage("root", core.TextContent{
		Text: "What time is it in Tokyo right now? Also, what's (25 * 4) + (100 / 5)?",
	})

	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("User: %s\n", userMsg.Content.(core.TextContent).Text)
	fmt.Println("───────────────────────────────────────────")

	messages, err := agent.Execute(ctx, systemMsg, []core.Message{userMsg})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
		os.Exit(1)
	}

	fmt.Println()

	// Tool calls/results are already shown via hooks (OnFunctionCall, OnToolCallEnd).
	// Only text content needs to be printed here — it arrives via OnReplyChunk in
	// stream mode and is aggregated in the final message; in block mode OnReplyChunk
	// never fires so this is the only text output.
	for _, msg := range messages {
		printMessage(msg)
	}
	if len(messages) == 0 {
		fmt.Println("(no response)")
	}
	fmt.Println("\n═══════════════════════════════════════════")
}

// ---- Provider factory ----

func createProvider(name, apiKey, baseURL, model string, thinking, stream bool) core.ILMProvider {
	fmt.Printf("provider : %s\n", name)
	fmt.Printf("model    : %s\n", model)
	fmt.Printf("stream   : %v\n", stream)
	fmt.Printf("thinking : %v\n", thinking)
	if baseURL != "" {
		fmt.Printf("baseurl  : %s\n", baseURL)
	}
	fmt.Println("───────────────────────────────────────────")
	switch name {
	case "openai":
		return provider.OpenAI(apiKey, baseURL, model)
	default:
		return provider.DeepSeek(apiKey, baseURL, model)
	}
}

func printUsageAndExit() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  go run . -apikey <key> [flags]\n\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  go run . -apikey sk-xxx\n")
	fmt.Fprintf(os.Stderr, "  go run . -apikey sk-xxx -provider openai -model gpt-4.1-mini\n")
	fmt.Fprintf(os.Stderr, "  go run . -apikey sk-xxx -thinking\n")
	fmt.Fprintf(os.Stderr, "  go run . -apikey sk-xxx -baseurl https://api.my-proxy.com\n")
	fmt.Fprintf(os.Stderr, "  go run . -apikey sk-xxx -stream=false\n")
	os.Exit(1)
}

// ---- Tool registration ----

func registerTools() {
	core.RegisterTool(
		core.ToolDefinition{
			Name:        "get_current_time",
			Description: "Get the current time in a given timezone. Use IANA timezone names like 'Asia/Shanghai', 'America/New_York', 'UTC'.",
			JsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"timezone": map[string]any{
						"type":        "string",
						"description": "IANA timezone name, e.g. 'Asia/Shanghai'",
					},
				},
				"required": []string{"timezone"},
			},
		},
		toolGetCurrentTime,
	)

	core.RegisterTool(
		core.ToolDefinition{
			Name:        "calculate",
			Description: "Evaluate a mathematical expression. Supports +, -, *, /, (), and basic math functions.",
			JsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "Math expression to evaluate, e.g. '(3 + 5) * 2 - 10 / 2'",
					},
				},
				"required": []string{"expression"},
			},
		},
		toolCalculate,
	)

	// This tool is registered globally but excluded via agent.WithTools().
	// If the LLM tries to call it, the agent won't see it.
	core.RegisterTool(
		core.ToolDefinition{
			Name:        "delete_file",
			Description: "Delete a file from the system. DANGEROUS — requires explicit confirmation.",
			JsonSchema:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}},
		},
		func(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
			return core.ToolCallResult{ToolCallID: detail.ID, ToolName: detail.ToolName, Outputs: core.MakeErrorToolOutputs(fmt.Errorf("access denied"))}
		},
	)
}

// ---- Tool implementations ----

func toolGetCurrentTime(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	timezone := "UTC"
	if tz, ok := detail.Args["timezone"].(string); ok && tz != "" {
		timezone = tz
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return core.ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    core.MakeErrorToolOutputs(fmt.Errorf("invalid timezone: %s", timezone)),
		}
	}

	now := time.Now().In(loc)
	return core.ToolCallResult{
		ToolCallID: detail.ID,
		ToolName:   detail.ToolName,
		Outputs: map[string]any{
			"timezone":  timezone,
			"time":      now.Format("15:04:05"),
			"date":      now.Format("2006-01-02"),
			"weekday":   now.Weekday().String(),
			"timestamp": now.Unix(),
		},
	}
}

func toolCalculate(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	expr, _ := detail.Args["expression"].(string)
	if expr == "" {
		return core.ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    core.MakeErrorToolOutputs(fmt.Errorf("expression is empty")),
		}
	}

	result, err := evalSimpleExpr(expr)
	if err != nil {
		return core.ToolCallResult{
			ToolCallID: detail.ID,
			ToolName:   detail.ToolName,
			Outputs:    core.MakeErrorToolOutputs(err),
		}
	}

	return core.ToolCallResult{
		ToolCallID: detail.ID,
		ToolName:   detail.ToolName,
		Outputs: map[string]any{
			"expression": expr,
			"result":     result,
		},
	}
}

// ---- Console event handler ----

type consoleEventHandler struct{}

func (h *consoleEventHandler) OnThinkingChunk(chunk string) {
	fmt.Print(chunk)
}

func (h *consoleEventHandler) OnReplyChunk(chunk string) {
	fmt.Print(chunk)
}

func (h *consoleEventHandler) OnFunctionCall(detail core.ToolCallDetail) {
	fmt.Printf("\n┌─ [call] %s\n", detail.ToolName)
	for k, v := range detail.Args {
		fmt.Printf("│  %s: %v\n", k, v)
	}
	fmt.Printf("└──────────────────────────────────────────\n")
}

func (h *consoleEventHandler) OnFinished(reason string) {
	fmt.Printf("\n[finished: %s]\n", reason)
}

func (h *consoleEventHandler) OnUsageUpdated(usage core.Usage) {
	fmt.Printf("[usage] in=%d out=%d reasoning=%d cached=%d total=%d\n",
		usage.InputTokens, usage.OutputTokens,
		usage.ReasoningTokens, usage.CachedTokens,
		usage.TotalTokens,
	)
}

func (h *consoleEventHandler) OnError(err error) {
	fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
}

// ---- Helpers ----

func printMessage(msg core.Message) {
	switch ct := msg.Content.(type) {
	case core.TextContent:
		fmt.Println(ct.Text)
	case core.ToolCallContent:
		for _, d := range ct.Details {
			fmt.Printf("[toolcall] %s(%v)\n", d.ToolName, d.Args)
		}
	case core.ToolResultContent:
		for _, r := range ct.Results {
			fmt.Printf("[toolresult] %s → %v\n", r.ToolName, r.Outputs)
		}
	case core.CompositeContent:
		for _, part := range ct.Parts {
			printMessage(core.Message{Content: part})
		}
	}
}

// ---- Expression evaluator ----

func evalSimpleExpr(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")

	var parseExpr func(s string) (float64, string, error)
	var parseTerm func(s string) (float64, string, error)
	var parseFactor func(s string) (float64, string, error)

	parseFactor = func(s string) (float64, string, error) {
		if len(s) == 0 {
			return 0, s, fmt.Errorf("unexpected end of expression")
		}
		if s[0] == '(' {
			val, rest, err := parseExpr(s[1:])
			if err != nil {
				return 0, rest, err
			}
			if len(rest) == 0 || rest[0] != ')' {
				return 0, rest, fmt.Errorf("missing closing parenthesis")
			}
			return val, rest[1:], nil
		}
		i := 0
		for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] == '-') {
			if s[i] == '-' && i > 0 {
				break
			}
			i++
		}
		if i == 0 {
			return 0, s, fmt.Errorf("expected number at %q", s)
		}
		var val float64
		if _, err := fmt.Sscanf(s[:i], "%f", &val); err != nil {
			return 0, s, err
		}
		return val, s[i:], nil
	}

	parseTerm = func(s string) (float64, string, error) {
		val, rest, err := parseFactor(s)
		if err != nil {
			return 0, rest, err
		}
		for len(rest) > 0 && (rest[0] == '*' || rest[0] == '/') {
			op := rest[0]
			right, rest2, err := parseFactor(rest[1:])
			if err != nil {
				return 0, rest2, err
			}
			if op == '*' {
				val *= right
			} else {
				if right == 0 {
					return 0, rest2, fmt.Errorf("division by zero")
				}
				val /= right
			}
			rest = rest2
		}
		return val, rest, nil
	}

	parseExpr = func(s string) (float64, string, error) {
		val, rest, err := parseTerm(s)
		if err != nil {
			return 0, rest, err
		}
		for len(rest) > 0 && (rest[0] == '+' || rest[0] == '-') {
			op := rest[0]
			right, rest2, err := parseTerm(rest[1:])
			if err != nil {
				return 0, rest2, err
			}
			if op == '+' {
				val += right
			} else {
				val -= right
			}
			rest = rest2
		}
		return val, rest, nil
	}

	val, rest, err := parseExpr(expr)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(rest) != "" {
		return 0, fmt.Errorf("unexpected trailing characters: %q", rest)
	}
	return val, nil
}
