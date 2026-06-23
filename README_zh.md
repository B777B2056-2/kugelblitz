# Kugelblitz

[English](README.md)

轻量、模块化的 **Agent Harness**（Go 语言）—— 提供 LLM Agent 运行所需的脚手架、
生命周期管理和可观测性基础设施。

## 核心特性

- **ReAct 引擎** — 思考→行动→观察循环，支持流式输出与工具调用
- **Planner + Worker 双代理** — 一个规划，多个并行执行
- **Plan-Execute-Adapt-Finish 工作流** — 带 checkpoint 和回滚的结构化执行
- **会话记忆** — 上下文窗口溢出时自动 LLM 压缩
- **长期记忆** — 语义去重，LLM 裁判解决冲突
- **目标漂移检测** — 定期审查，偏离目标时自动回滚
- **Skills 插件** — 可插拔的领域知识模块
- **Langfuse 可观测性** — 完整 trace / span / generation 层级开箱即用
- **统一 Usage 回调** — 所有 LLM 调用的 token 消耗通过单一回调上报，按来源标识

## 架构概览

```
                         ┌──────────────────────────────┐
                         │       Kugelblitz Harness      │
 用户目标 ──────────────►│                               │
                         │  ┌─────────────────────────┐  │
                         │  │ Planner                 │  │
                         │  │ 规划 → 执行 →          │  │
                         │  │ 适配 → 完成            │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ 工具底座                │  │
                         │  │ plan_*, task_*,         │  │
                         │  │ memory_*, skill_use,    │  │
                         │  │ worker_spawn            │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ 记忆底座                │  │
                         │  │ 会话 + 长期记忆         │  │
                         │  │ + 自动压缩              │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ 审查底座                │  │
                         │  │ 漂移检测                │  │
                         │  │ + 自动回滚              │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ 可观测性                │  │
                         │  │ Langfuse 追踪 +         │  │
                         │  │ 统一 usage 回调         │  │
                         │  └─────────────────────────┘  │
                         └──────────────────────────────┘
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"

    "kugelblitz/core"
    "kugelblitz/provider"
    "kugelblitz/runtime"
)

func main() {
    p := provider.DeepSeek("sk-xxx", "https://api.deepseek.com", "deepseek-v4-flash")
    planner := runtime.NewPlanner(p, true /* streaming */)

    msgs, err := planner.Execute(context.Background(),
        "创建一个 README.md 描述项目功能，再创建 docs/architecture.md 描述架构")
    if err != nil {
        panic(err)
    }
    for _, m := range msgs {
        if tc, ok := m.Content.(core.TextContent); ok {
            fmt.Println(tc.Text)
        }
    }
}
```

可选接入 Langfuse：

```bash
go run . -apikey sk-xxx \
  -langfuse-host http://localhost:3000 \
  -langfuse-pk pk-xxx \
  -langfuse-sk sk-xxx
```

## 核心概念

### Planner（规划器）

Planner 遵循 **Plan → Execute → Adapt → Finish** 工作流。它将用户目标拆解为子任务，
调度 Worker 并行执行独立任务，并持续监控进度。失败的任务触发适配（重新规划或重试）。
全部完成后 Planner 汇总并结束。

### Worker（执行器）

Worker 执行独立的子任务，拥有受限的工具集（文件读写、目录操作）。
Planner 通过 `worker_spawn` 创建 Worker，独立任务**并发执行**。

### 记忆底座

- **会话记忆** — 完整对话历史。上下文窗口溢出时（`ErrContextLengthExceeded`），
  底座**自动将旧消息压缩**为 LLM 摘要，保留最近 N 条消息。

- **长期记忆** — 从完成的对话中提取关键事实与经验，经**语义去重**后持久化。
  新信息与已有记忆冲突时由 LLM 裁判解决。

### Skills（技能模块）

Skills 是可插拔的 YAML/SKILL.md 模块，激活后自动注入 Planner 的系统提示词。
通过 `skill_use` 工具激活，底座自动加载其指令和工具定义。

## Harness — 自愈 & 纠偏

底座内置了两项**无需人工介入**的安全机制，确保执行始终围绕目标。

### Planner 自愈

当工具调用连续失败达到阈值（`FailuresBeforeReview`），Planner **自动回滚**
到上一个正常 checkpoint：

```
第 N 步   ──❌ 失败
第 N+1 步 ──❌ 失败
第 N+2 步 ──❌ 失败
    → FailuresBeforeReview（默认 3）达到
    → 加载第 N-1 个版本的 checkpoint
    → 将 Plan 恢复到该 checkpoint
    → 注入系统消息："plan rolled back to vN-1"
    → 从安全点继续执行
```

这避免了模型在错误的路径上叠加错误，给了它一条干净的恢复路径——
**全程无需人工介入**。

配置方式：

```go
planner.SetReviewConfig(runtime.ReviewConfig{
    FailuresBeforeReview: 3,  // 连续失败 3 次触发
})
```

### Reviewer 漂移检测

两种条件独立触发 Reviewer 审查：

1. **ReAct 步数间隔** — 每 N 步审查一次（`ReActStepInterval`），不论成功与否。
2. **连续失败** — 连续失败 N 次（`FailuresBeforeReview`）时审查。

Reviewer 将**原始目标**、**当前计划状态**和**近期活动**发给 LLM，
LLM 通过 `reviewer_report` 工具返回结构化判断：

```go
type ReviewResult struct {
    Drift      bool        // 是否偏离目标
    Reason     string      // 偏离原因
    Suggestion string      // 建议修正措施
}
```

若 `Drift == true`，底座自动触发 `replan()`，回滚到上一个 checkpoint 版本——
与自愈路径一致。

这确保 Planner 始终聚焦**原始目标**，避免在无关或范围蔓延的任务上浪费 token。

配置方式：

```go
planner.SetReviewConfig(runtime.ReviewConfig{
    ReActStepInterval:    8,  // 每 8 个 ReAct 步审查一次
    FailuresBeforeReview: 3,  // 连续失败 3 次也触发审查
})
```

### Harness 流程总览

```
        ┌──────────┐
        │  Execute  │
        └─────┬─────┘
              │
    ┌─────────▼─────────┐
    │  ReAct 步骤循环    │◄──────────────────────────┐
    └─────────┬─────────┘                           │
              │                                     │
    ┌─────────▼─────────┐     ┌──────────────┐      │
    │  工具执行结果      │────►│  是否失败？  │      │
    └───────────────────┘     └──────┬───────┘      │
                                     │              │
                          ┌──────────▼──────────┐   │
                          │  连续失败 ≥ 阈值？  │   │
                          └──────────┬───────────┘   │
                                     │              │
                          ┌──────────▼──────────┐   │
                          │  Reviewer 检查      │   │
                          │  是否漂移目标       │   │
                          └──────────┬──────────┘   │
                                     │              │
                          ┌──────────▼──────────┐   │
                          │  检测到漂移？       │───┤
                          │  → replan(回滚)     │   │
                          └─────────────────────┘   │
                                                    │
                          ┌─────────────────────────┘
                          │
                ┌─────────▼─────────┐
                │  步数间隔 ≥       │
                │  ReActStep        │──► Reviewer 检查
                │  Interval？       │
                └───────────────────┘
```

## 可观测性

### Langfuse 追踪

Planner 执行自动生成完整层级：

```
Trace "session-<uuid>"
  ├── Span "react.step" #1
  │   ├── Generation "step-1-llm"     ← 输入/输出 token
  │   ├── Span "tool:plan_create"     ← 入参 + 执行结果
  │   └── Span "tool:task_insert"
  ├── Span "context.compress"         ← 压缩摘要 + token 用量
  ├── Span "react.step" #2
  │   ├── Generation "step-2-llm"
  │   └── Span "tool:worker_spawn"
  └── Span "reviewer.check"           ← 漂移评估 + token 用量
```

一行配置即可接入：

```go
lfObs := observability.NewLangfuseObserver(observability.LangfuseConfig{
    Host:      "http://localhost:3000",
    PublicKey: "pk-xxx",
    SecretKey: "sk-xxx",
})
planner := runtime.NewPlanner(p, true, runtime.WithObserver(lfObs))
```

### 统一 Usage 回调

单一回调即可获取**全部 LLM 调用**的 token 消耗，按来源区分：

```go
planner := runtime.NewPlanner(p, true,
    runtime.WithLLMUsageCallback(func(report core.LLMUsageReport) {
        fmt.Printf("[%s] in=%d out=%d total=%d\n",
            report.Identity,
            report.Usage.InputTokens,
            report.Usage.OutputTokens,
            report.Usage.TotalTokens,
        )
    }),
)
```

回调中的 Identity 标识：`planner.step-1`、`planner.step-2`、`compressor`、
`reviewer`、`worker.<task-id>`。

## 目录结构

```
kugelblitz/
├── core/              # 接口定义：Observer, Span, Message, Tool, Workspace
├── runtime/           # Planner, ReactAgent, WorkerAgent, Reviewer
├── memory/            # SessionMemory, Compressor, LongTermMemory
├── observability/     # LangfuseObserver, PlannerInstrument
├── tools/
│   └── internals/     # plan_*, task_*, memory_*, worker_spawn, skill_use
├── skills/            # Skill 加载与注册
├── provider/
│   └── chat_completions/  # OpenAI 兼容格式 (Block + Stream)
├── persist/           # Plan checkpoint JSON, 会话 JSONL
├── utils/             # UUID 生成, session ID
└── examples/
    ├── plan_mode/     # Planner 完整示例
    ├── react/         # 独立 ReAct agent
    └── drift_demo/    # 漂移检测示例
```
