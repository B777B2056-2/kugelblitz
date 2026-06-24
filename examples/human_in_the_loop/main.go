// Example: Human-in-the-loop with ask_human tool.
//
// Demonstrates how the agent can pause execution and ask the human user for
// input via the OnWaitForHumanAction callback and ResumeWithHumanResponse.
//
// Usage:
//
//	go run . -apikey sk-xxx
//	go run . -apikey sk-xxx -model deepseek-v4-flash
//	go run . -apikey sk-xxx -provider openai -model gpt-4.1-mini
package main

import (
	"bufio"
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
	baseURL := flag.String("baseurl", "https://api.deepseek.com", "Custom API base URL")
	enableThinking := flag.Bool("thinking", true, "Enable thinking/reasoning mode")
	streamMode := flag.Bool("stream", true, "Streaming mode")
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintf(os.Stderr, "Usage: go run . -apikey <key> [flags]\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// ---- 2. Create provider ----
	p := createProvider(*providerChoice, *apiKey, *baseURL, *model)

	// ---- 3. Register tools ----
	registerTools()

	// ---- 4. Create agent with HITL enabled ----
	agent := runtime.NewReactAgent(p, *streamMode)
	if *enableThinking {
		agent.SetThinking(true, core.ReasoningEffortHigh)
	}
	agent.WithTools("get_current_time", "calculate", "schedule_meeting", "ask_human")
	agent.EnableHumanInTheLoop()

	// ---- 5. Register event hooks (including HITL callback) ----
	waitSig := make(chan struct{}, 1) // signals main goroutine to read stdin
	agent.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: &consoleEventHandler{},
		OnToolCallEnd: func(result core.ToolCallResult) {
			fmt.Printf("\n┌─ [tool] %s ─────────────────────────────\n", result.ToolName)
			for k, v := range result.Outputs {
				fmt.Printf("│  %s: %v\n", k, v)
			}
			fmt.Printf("└──────────────────────────────────────────\n")
		},
		OnWaitForHumanAction: func(reason, prompt string) {
			fmt.Println()
			fmt.Println("┌──────────────────────────────────────────")
			fmt.Printf("│ 🤖 Agent needs your input\n")
			if reason != "" {
				fmt.Printf("│ Reason: %s\n", reason)
			}
			fmt.Printf("│ Question: %s\n", prompt)
			fmt.Printf("│ Your response: ")

			// Signal the main goroutine that we need input
			select {
			case waitSig <- struct{}{}:
			default:
			}
		},
	})

	// ---- 6. Run the agent in a goroutine ----
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	systemMsg := core.NewUserMessage("root", core.TextContent{
		Text: "You are a helpful assistant with human-in-the-loop. " +
			"Use tools when helpful, then answer in the user's language. " +
			"CRITICAL: before calling schedule_meeting (which sends calendar invites), " +
			"you MUST call ask_human to get the user's explicit approval.",
	})
	systemMsg.Role = "system"

	userMsg := core.NewUserMessage("root", core.TextContent{
		Text: "Check the current time in Tokyo. Then propose scheduling a short team standup for tomorrow, " +
			"but make sure to ask me for confirmation before actually sending the calendar invite.",
	})

	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("User: %s\n", userMsg.Content.(core.TextContent).Text)
	fmt.Println("───────────────────────────────────────────")

	var messages []core.Message
	var execErr error
	go func() {
		messages, execErr = agent.Execute(ctx, systemMsg, []core.Message{userMsg})
		cancel() // signal main goroutine that agent is done
	}()

	// ---- 7. Handle human-in-the-loop prompts from stdin ----
	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-waitSig:
			scanner.Scan()
			response := scanner.Text()
			fmt.Printf("└──────────────────────────────────────────\n")

			if err := agent.ResumeWithHumanResponse(ctx, response); err != nil {
				fmt.Fprintf(os.Stderr, "[error] resume failed: %v\n", err)
			}
		case <-ctx.Done():
			goto done
		}
	}
done:
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "[error] reading input: %v\n", err)
	}

	// ---- 8. Print final result ----
	if execErr != nil {
		fmt.Fprintf(os.Stderr, "\n[error] %v\n", execErr)
		os.Exit(1)
	}

	fmt.Println()
	for _, msg := range messages {
		printMessage(msg)
	}
	fmt.Println("\n═══════════════════════════════════════════")
}

// ---- Provider factory ----

func createProvider(name, apiKey, baseURL, model string) core.ILMProvider {
	fmt.Printf("provider : %s\n", name)
	fmt.Printf("model    : %s\n", model)
	fmt.Println("───────────────────────────────────────────")
	switch name {
	case "openai":
		return provider.OpenAI(apiKey, baseURL, model)
	default:
		return provider.DeepSeek(apiKey, baseURL, model)
	}
}

// ---- Tool registration ----

func registerTools() {
	core.RegisterTool(
		core.ToolDefinition{
			Name:        "get_current_time",
			Description: "Get the current time in a given timezone. Use IANA timezone names like 'Asia/Tokyo', 'America/New_York', 'UTC'.",
			JsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"timezone": map[string]any{
						"type":        "string",
						"description": "IANA timezone name, e.g. 'Asia/Tokyo'",
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
			Description: "Evaluate a mathematical expression. Supports +, -, *, /, ().",
			JsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "Math expression to evaluate, e.g. '2 + 3 * 4'",
					},
				},
				"required": []string{"expression"},
			},
		},
		toolCalculate,
	)

	core.RegisterTool(
		core.ToolDefinition{
			Name:        "schedule_meeting",
			Description: "Schedule a meeting and send calendar invites. IMPORTANT: you MUST call ask_human for approval before using this tool.",
			JsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "Meeting title",
					},
					"time": map[string]any{
						"type":        "string",
						"description": "Meeting time, e.g. '2026-06-24 15:00'",
					},
					"duration_minutes": map[string]any{
						"type":        "integer",
						"description": "Meeting duration in minutes",
					},
				},
				"required": []string{"title", "time", "duration_minutes"},
			},
		},
		toolScheduleMeeting,
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
			ToolCallID: detail.ID, ToolName: detail.ToolName,
			Outputs: core.MakeErrorToolOutputs(fmt.Errorf("invalid timezone: %s", timezone)),
		}
	}

	now := time.Now().In(loc)
	return core.ToolCallResult{
		ToolCallID: detail.ID, ToolName: detail.ToolName,
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
			ToolCallID: detail.ID, ToolName: detail.ToolName,
			Outputs: core.MakeErrorToolOutputs(fmt.Errorf("expression is empty")),
		}
	}
	result, err := evalSimpleExpr(expr)
	if err != nil {
		return core.ToolCallResult{
			ToolCallID: detail.ID, ToolName: detail.ToolName,
			Outputs: core.MakeErrorToolOutputs(err),
		}
	}
	return core.ToolCallResult{
		ToolCallID: detail.ID, ToolName: detail.ToolName,
		Outputs: map[string]any{"expression": expr, "result": result},
	}
}

func toolScheduleMeeting(ctx context.Context, detail core.ToolCallDetail) core.ToolCallResult {
	title, _ := detail.Args["title"].(string)
	meetingTime, _ := detail.Args["time"].(string)
	duration, _ := detail.Args["duration_minutes"].(float64)

	return core.ToolCallResult{
		ToolCallID: detail.ID, ToolName: detail.ToolName,
		Outputs: map[string]any{
			"title":            title,
			"time":             meetingTime,
			"duration_minutes": int(duration),
			"status":           "confirmed",
			"calendar_link":    fmt.Sprintf("https://calendar.example.com/meeting/%s", detail.ID),
		},
	}
}

// ---- Console event handler ----

type consoleEventHandler struct{}

func (h *consoleEventHandler) OnThinkingChunk(chunk string)  { fmt.Print(chunk) }
func (h *consoleEventHandler) OnReplyChunk(chunk string)     { fmt.Print(chunk) }
func (h *consoleEventHandler) OnFunctionCall(detail core.ToolCallDetail) {
	fmt.Printf("\n┌─ [call] %s\n", detail.ToolName)
	for k, v := range detail.Args {
		fmt.Printf("│  %s: %v\n", k, v)
	}
	fmt.Printf("└──────────────────────────────────────────\n")
}
func (h *consoleEventHandler) OnFinished(reason string) {}
func (h *consoleEventHandler) OnUsageUpdated(usage core.Usage) {
	fmt.Printf("[usage] in=%d out=%d total=%d\n", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
}
func (h *consoleEventHandler) OnError(err error) {}

// ---- Helpers ----

func printMessage(msg core.Message) {
	switch ct := msg.Content.(type) {
	case core.TextContent:
		fmt.Println(ct.Text)
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
			return 0, s, fmt.Errorf("unexpected end")
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
