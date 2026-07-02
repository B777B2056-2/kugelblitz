// Drift detection demo: complex long-running task with goal drift.
//
// Usage:
//
//	go run . -apikey sk-xxx
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/provider"
	"github.com/B777B2056-2/kugelblitz/runtime"
	_ "github.com/B777B2056-2/kugelblitz/tools/internals"
)

func main() {
	apiKey := flag.String("apikey", "", "DeepSeek API key (required)")
	model := flag.String("model", "deepseek-chat", "Model name")
	streamMode := flag.Bool("stream", true, "Streaming output")
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Usage: go run . -apikey sk-xxx [-model deepseek-chat] [-stream=true]")
		os.Exit(1)
	}

	p := provider.DeepSeek(*apiKey, "https://api.deepseek.com", *model)

	// Create Planner with aggressive review thresholds for demo
	planner := runtime.NewPlanner(p, *streamMode)

	// Override review config to demonstrate drift detection sooner
	planner.SetReviewConfig(runtime.ReviewConfig{
		ReActStepInterval:    5, // review every 5 steps
		FailuresBeforeReview: 2, // review after 2 consecutive failures
	})

	planner.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: &consoleHandler{startTime: time.Now()},
		OnToolCallEnd: func(result core.ToolCallResult) {
			if errMsg, hasErr := result.Outputs["error"]; hasErr {
				fmt.Printf("\n   [tool error] %s: %v\n", result.ToolName, errMsg)
			}
		},
	})

	ctx := context.Background()

	goal := `从零开始在项目根目录创建一个Python技术博客项目，具体要求：

1. 项目结构：
   - blog/ 为主目录
   - blog/app.py FastAPI入口
   - blog/models.py 数据模型（Post表：id/title/content/created_at）
   - blog/database.py SQLite数据库初始化
   - blog/schemas.py Pydantic请求响应模型
   - requirements.txt 依赖清单
   - README.md 项目说明
   - tests/test_app.py 基础测试

2. 核心功能：
   - GET /posts 获取所有文章
   - POST /posts 创建新文章
   - GET /posts/{id} 获取单篇文章

3. 编写完整的API文档在 blog/API.md

4. 注意：这是一个中等复杂度的任务，请务必规划好再执行。`

	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("Drift Detection Demo")
	fmt.Printf("Goal: %s\n", goal[:80]+"...")
	fmt.Println()
	fmt.Println("Review Config: step_interval=5, failures_before_review=2")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Println()

	msgs, err := planner.Execute(ctx, goal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("Planner finished. Messages:")
	for i, msg := range msgs {
		if tc, ok := msg.Content.(core.TextContent); ok {
			fmt.Printf("[%d] %s\n", i, tc.Text[:min(200, len(tc.Text))])
		} else if tcc, ok := msg.Content.(core.ToolCallContent); ok {
			for _, d := range tcc.Details {
				fmt.Printf("[%d] tool: %s(%v)\n", i, d.ToolName, d.Args)
			}
		}
	}
}

type consoleHandler struct {
	startTime time.Time
}

func (h *consoleHandler) OnThinkingChunk(chunk string) {}
func (h *consoleHandler) OnReplyChunk(chunk string)    { fmt.Print(chunk) }
func (h *consoleHandler) OnFunctionCall(detail core.ToolCallDetail) {
	switch detail.ToolName {
	case "plan_create", "plan_query", "plan_rollback", "confirm_plan",
		"task_insert", "task_delete", "task_query", "worker_spawn":
		fmt.Printf("\n[%s]\n", detail.ToolName)
	default:
		fmt.Printf("\n  [call] %s\n", detail.ToolName)
	}
}
func (h *consoleHandler) OnFinished(reason string) {
	fmt.Printf("  [finished: %s]\n", reason)
}
func (h *consoleHandler) OnUsageUpdated(usage core.Usage) {
	fmt.Printf("  [tokens: in=%d out=%d total=%d]\n",
		usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
}
func (h *consoleHandler) OnError(err error) {
	fmt.Printf("  [error: %v]\n", err)
}
