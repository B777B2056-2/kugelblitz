// eval-cli is a thin CLI adapter that runs a single Kugelblitz AgentLoop
// execution and outputs structured JSON lines to stdout. It is invoked by
// the Python eval orchestrator via subprocess.
//
// Usage:
//
//	eval-cli run --session-id <id> --goal <text> [--workdir <dir>] [--config <path>]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/B777B2056-2/kugelblitz/cmd/common"
	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/runtime"
)

func main() {
	sessionID := flag.String("session-id", "", "framework session ID (required)")
	goal := flag.String("goal", "", "task goal text (required)")
	workdir := flag.String("workdir", "", "working directory for agent execution")
	configPath := flag.String("config", "eval/config.yaml", "path to kugelblitz config YAML")
	flag.Parse()

	if *sessionID == "" || *goal == "" {
		fmt.Fprintln(os.Stderr, "usage: eval-cli run --session-id <id> --goal <text> [--workdir <dir>] [--config <path>]")
		os.Exit(2)
	}

	// Load eval-specific config; fall back to defaults if missing
	cfg, err := common.Load(*configPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Initialize OTel (noop if disabled)
	shutdown, _ := observability.InitTracer(context.Background(), cfg.Observability)
	defer shutdown()

	if *workdir != "" {
		core.GetWorkspace().SetDir(*workdir)
	}

	opts := []runtime.AgentLoopOption{
		runtime.WithExistingSessionID(*sessionID),
	}

	loop := runtime.NewAgentLoop(cfg, opts...)

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)

	loop.RegisterEventHooks(core.AgentEventHooks{
		OnReplyChunk: func(id constants.AgentIdentity, chunk string) {
			enc.Encode(jsonEvent("reply", map[string]any{
				"identity": string(id),
				"text":     chunk,
			}))
		},
		OnBlockReply: func(id constants.AgentIdentity, text string) {
			enc.Encode(jsonEvent("reply_block", map[string]any{
				"identity": string(id),
				"text":     text,
			}))
		},
		OnFunctionCall: func(id constants.AgentIdentity, detail core.ToolCallDetail) {
			enc.Encode(jsonEvent("tool_call", map[string]any{
				"identity":      string(id),
				"tool_call_id":  detail.ID,
				"tool_name":     detail.ToolName,
				"args":          detail.Args,
			}))
		},
		OnToolCallEnd: func(id constants.AgentIdentity, result core.ToolCallResult) {
			enc.Encode(jsonEvent("tool_result", map[string]any{
				"identity":     string(id),
				"tool_call_id": result.ToolCallID,
				"tool_name":    result.ToolName,
				"output":       result.Outputs,
			}))
		},
		OnTaskUpdated: func(id constants.AgentIdentity, taskID, goal, status, output string) {
			enc.Encode(jsonEvent("task_updated", map[string]any{
				"identity": string(id),
				"task_id":  taskID,
				"goal":     goal,
				"status":   status,
				"output":   output,
			}))
		},
		OnModelFinished: func(id constants.AgentIdentity, reason string) {
			enc.Encode(jsonEvent("finished", map[string]any{
				"identity": string(id),
				"reason":   reason,
			}))
		},
		OnUsageUpdated: func(id constants.AgentIdentity, usage core.Usage) {
			enc.Encode(jsonEvent("usage", map[string]any{
				"identity":  string(id),
				"input":     usage.InputTokens,
				"output":    usage.OutputTokens,
				"reasoning": usage.ReasoningTokens,
				"total":     usage.TotalTokens,
			}))
		},
		OnError: func(id constants.AgentIdentity, err error) {
			enc.Encode(jsonEvent("error", map[string]any{
				"identity": string(id),
				"message":  err.Error(),
			}))
		},
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() { <-loop.Done(); cancel() }()
	loop.Run(ctx, core.AgentInput{Text: *goal})
	<-ctx.Done()

	// Emit plan snapshot at end
	for _, plan := range working.ListPlans() {
		tasks := make([]map[string]any, len(plan.SubTasks))
		for i, t := range plan.SubTasks {
			tasks[i] = map[string]any{
				"id":     t.ID,
				"goal":   t.Goal,
				"status": string(t.Status),
			}
		}
		enc.Encode(jsonEvent("plan_snapshot", map[string]any{
			"plan_id": plan.ID,
			"name":    plan.Name,
			"status":  string(plan.State),
			"tasks":   tasks,
		}))
	}

	// Emit done
	enc.Encode(jsonEvent("done", map[string]any{
		"session_id": loop.SessionID(),
	}))
}

func jsonEvent(event string, data map[string]any) map[string]any {
	data["event"] = event
	return data
}

