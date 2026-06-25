# Kugelblitz

[English](README.md)

轻量、模块化的 **Harness Agent**—— 提供 Agent 运行所需的脚手架、
生命周期管理和可观测性基础设施。

## 核心特性

- **ReAct 引擎** — 思考→行动→观察循环，支持流式输出与工具调用
- **Planner + Worker 双代理** — 一个规划，多个并行执行
- **Plan-Execute-Adapt-Finish 工作流** — 带 checkpoint 和回滚的结构化执行
- **会话记忆** — 上下文窗口溢出时自动 LLM 压缩
- **长期记忆** — 语义去重，LLM 裁判解决冲突
- **目标漂移检测** — 定期审查，偏离目标时自动回滚
- **Skills 插件** — 可插拔的领域知识模块
- **内置 Web 工具** — `web_search`（DuckDuckGo 零配置）+ `web_fetch`（HTML → Markdown，可选动态渲染）
- **人在回路** — agent 可主动暂停征询人类意见，通过 `OnWaitForHumanAction` 回调 + `ResumeWithHumanResponse` 恢复
- **Langfuse 可观测性** — 完整 trace / span / generation 层级开箱即用
- **统一 Usage 回调** — 所有 LLM 调用的 token 消耗通过单一回调上报，按来源标识
- **ACP（Agent Client Protocol）** — JSON-RPC 2.0 over stdio 适配器，按需启用。
  任何支持 ACP 的编辑器（Zed、JetBrains、VS Code、Neovim）均可作为
  Kugelblitz Agent 的前端界面

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

    "github.com/B777B2056-2/kugelblitz/core"
    "github.com/B777B2056-2/kugelblitz/provider"
    "github.com/B777B2056-2/kugelblitz/runtime"
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

### 网络工具

内置互联网访问工具，一行注册即可使用：

```go
internals.RegisterWebTools(nil) // DuckDuckGo（免费，无需 API Key）
```

**`web_search`** — 通过 DuckDuckGo 搜索网页，零配置。返回结构化结果
（标题、URL、摘要）。可通过 `SearchBackend` 接口接入自定义后端（如 Brave Search）。

**`web_fetch`** — 抓取网页并转换为干净的 Markdown，自动去除 script/style/导航栏。
设置 `render_js: true` 可使用无头浏览器渲染 SPA 后再提取。

```
web_search (query, limit?)   → {query, results[{title, url, snippet}]}
web_fetch  (url, render_js?) → {url, title, markdown}
```

## ACP — Agent Client Protocol

Kugelblitz 可作为 ACP-compatible Agent 运行，接入任何支持
[Agent Client Protocol](https://agentclientprotocol.com) 的编辑器（Zed、JetBrains、
VS Code、Neovim 等）。

### 协议流程

```
编辑器 (Client)               Kugelblitz (Server)
     │                              │
     ├─ initialize ────────────────>│  版本协商 + 能力声明
     │<─ initialize result ────────┤
     ├─ session/new ───────────────>│  创建会话 (cwd)
     │<─ session/new result ───────┤  (返回 sessionId)
     ├─ session/prompt ────────────>│  用户消息
     │<─ session/update ────────────┤  流式文本块
     │<─ session/update ────────────┤  工具调用 + 结果
     │<─ session/prompt result ─────┤  end_turn / cancelled
     ├─ session/cancel ────────────>│  中断
     ├─ session/load ──────────────>│  恢复历史会话
     ├─ session/list ──────────────>│  列出所有会话
```

### 支持的方法

| 方法 | 方向 | 说明 |
|------|------|------|
| `initialize` | C→A | 协议版本 + 能力协商 |
| `session/new` | C→A | 创建会话，指定工作目录 |
| `session/prompt` | C→A | 发送用户 Prompt |
| `session/cancel` | C→A | 取消当前执行 |
| `session/load` | C→A | 加载并重放历史会话 |
| `session/list` | C→A | 列出所有活跃会话 |
| `session/delete` | C→A | 删除会话 |
| `session/update` | A→C | 流式通知（文本块、工具调用） |

### 快速开始

```go
p := provider.DeepSeek("sk-xxx", "https://api.deepseek.com", "deepseek-v4-flash")
agent := runtime.NewReactAgent(p, true)

srv := acp.NewServer(agent, p)
srv.Run(context.Background())
```

```bash
go run examples/acp_server/main.go -apikey sk-xxx
```

编辑器配置示例：

```json
{
  "agent": {
    "command": "go",
    "args": ["run", "examples/acp_server", "-apikey", "sk-xxx"],
    "work_dir": "/path/to/kugelblitz"
  }
}
```

完整示例见 [examples/acp_server/](examples/acp_server/)。

## 人在回路 (Human-in-the-Loop)

底座支持 **agent 主动暂停执行，征询人类意见，待回复后继续**。通过内置的 `ask_human` 工具实现，对 LLM 来说它只是一个普通的工具调用——但执行时会阻塞整个 ReAct 循环直到人类回复。

### 机制原理

```
ReAct 循环
  │
  ├── 思考 → 调用工具 A
  ├── 思考 → 调用工具 B
  ├── 思考 → 调用 ask_human(question="要删除这个文件吗?", reason="需确认")
  │           │
  │           ├─ 触发 OnWaitForHumanAction 回调  ← 通知外部（微信/UI/控制台）
  │           ├─ 阻塞等待人类回复...              ← 暂停点
  │           │     ↑
  │           │     │ ResumeWithHumanResponse(ctx, "同意删除")
  │           │     │
  │           └─ 收到回复，返回 {"response":"同意删除"} 给 LLM
  │
  ├── 思考 → 调用 tool_C（根据人类回复继续）
  └── 返回最终结果
```

### 核心接口

| 接口 | 说明 |
|------|------|
| `core.HumanGate` | `WaitForHuman(ctx, reason, prompt) (string, error)` — tool 通过此接口触发暂停 |
| `OnWaitForHumanAction(reason, prompt)` | `AgentEventHooks` 回调：进入等待状态时触发 |
| `agent.EnableHumanInTheLoop()` | 启用 HITL，注册 `ask_human` 本地工具 |
| `agent.HumanLoopWaiting() bool` | 查询是否正在等待 |
| `agent.ResumeWithHumanResponse(ctx, reply)` | 注入人类回复，解除阻塞 |

### 关键设计

- **对 LLM 透明** — `ask_human` 和普通工具完全一致：有 tool definition、有 tool result、`OnToolCallEnd` 回调正常触发
- **`ToolCallResult` 零改动** — 暂停机制完全封装在工具执行内部，不侵入任何现有类型
- **`Interrupt` 零改动** — `Interrupt()` 只管理 `abortSignal`；暂停通过 context 取消解除
- **本地工具注册** — `ask_human` 不在全局注册表，而是每个 agent 实例独立注册，持有对该 agent 的 `HumanGate` 引用
- **Planner / Worker 默认启用** — `NewPlanner` 和 `NewWorkerAgent` 自动注册 `ask_human`，开箱即用

### 示例

```go
agent := runtime.NewReactAgent(p, true)
agent.EnableHumanInTheLoop()

waitSig := make(chan struct{}, 1)
agent.RegisterEventHooks(core.AgentEventHooks{
    OnWaitForHumanAction: func(reason, prompt string) {
        fmt.Printf("🤖 Agent asks: %s\n", prompt)
        waitSig <- struct{}{}
    },
})

ctx, cancel := context.WithCancel(context.Background())
go func() { agent.Execute(ctx, sysMsg, userMsgs); cancel() }()

for {
    select {
    case <-waitSig:
        var reply string
        fmt.Scanln(&reply)
        agent.ResumeWithHumanResponse(ctx, reply)
    case <-ctx.Done():
        return
    }
}
```

完整代码见 [examples/human_in_the_loop/](examples/human_in_the_loop/)。

## Agent 上下文文件

底座启动时会从 `~/.kugelblitz/` 目录自动加载以下文件，并注入 System Prompt：

| 文件 | 用途 |
|------|------|
| `AGENTS.md` | Agent 能力声明 |
| `IDENTITY.md` | Agent 身份定义 |
| `SOUL.md` | Agent 性格/语气 |
| `USER.md` | 用户偏好/档案 |

缺失或为空的文件会被静默跳过。这是一种 **零代码的 Agent 定制方式**——将文件放入 workspace 目录即可改变 agent 行为。

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

### 工具结果压缩

当工具返回超长字符串值（如 `file_read` 读了 1 万行文件、`web_fetch` 抓了长网页），
底座**自动将该字段用 LLM 压缩**再注入对话上下文 — 避免浪费 token 和溢出上下文窗口。

- **按字段压缩** — 仅对超过阈值的单个字符串 value 压缩；路径、数字、布尔、短字符串原样保留。
- **错误安全** — 包含 `error` 字段的结果整体跳过。
- **可配置** — 阈值通过 `WithMaxToolResultChars` 设置（默认 4000 个 UTF-8 字符，0 禁用）。

```go
planner := runtime.NewPlanner(p, true,
    runtime.WithMaxToolResultChars(2000), // 字符串超过 2000 字符则压缩
)
```

压缩后的值直接替换 Outputs 中的原字段，Planner 看到的是摘要，
而 Langfuse 中的工具 span 仍保留原始记录。

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
├── acp/               # ACP 适配器（JSON-RPC 2.0 stdio 传输、会话管理）
├── tools/
│   └── internals/     # plan_*, task_*, memory_*, worker_spawn, skill_use
├── skills/            # Skill 加载与注册
├── provider/
│   └── chat_completions/  # OpenAI 兼容格式 (Block + Stream)
├── persist/           # Plan checkpoint JSON, 会话 JSONL
├── utils/             # UUID 生成, session ID
└── examples/
    ├── plan_mode/            # Planner 完整示例
    ├── react/                # 独立 ReAct agent
    ├── acp_server/           # ACP 服务端（编辑器兼容 Agent）
    ├── drift_demo/           # 漂移检测示例
    └── human_in_the_loop/    # 人在回路示例
```
