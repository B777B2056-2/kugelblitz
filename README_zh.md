# Kugelblitz

[English](README.md)

轻量、模块化的 Go Harness Agent ——通过智能路由、FSM 状态 Harness 和 DAG 任务编排，
让 LLM Agent 行为可控、可审计、可干预。

## 核心特性

- **🧭 智能路由** — 单次 LLM 结构化调用完成意图分类：简单任务 → Direct 直通执行，复杂任务 → Plan 完整 FSM 生命周期
- **⚙️ Agent 状态 Harness** — FSM 强制约束 Agent 生命周期，9 状态流转可控可审计，详见 [Harness — 自愈 & 纠偏](#harness--自愈--纠偏)
- **📋 DAG 任务编排** — 拓扑排序 + 批次并发执行，失败隔离 + 漂移回调
- **ReAct 引擎** — think → act → observe 循环，支持流式 + 按状态工具白名单
- **三层记忆体系** — Session（会话 + 自动压缩）+ Working（Plan/Task/Checkpoint）+ Long-Term（MEMORY.md + ChromaDB + 知识图谱 + 后台 Dreaming 巩固）
- **目标漂移检测** — Reviewer 定期审查执行轨迹，偏离目标时自动 checkpoint 回滚
- **Human-in-the-Loop** — `ask_human` 工具暂停执行，DAG 全局同步暂停，`OnWaitForHumanAction` + `ResumeWithHumanResponse` 恢复
- **Skills 插件** — 可插拔 YAML/SKILL.md 领域知识模块
- **内置 Web 工具** — `web_search`（DuckDuckGo 零配置）+ `web_fetch`（HTML→Markdown，可选 JS 渲染）
- **可观测性** — OpenTelemetry 全链路追踪，完整 trace/span/generation 层级
- **统一 Usage 回调** — 所有 LLM token 消耗通过单一回调上报，按来源标识
- **ACP 协议** — JSON-RPC 2.0 over stdio，接入 Zed / JetBrains / VS Code / Neovim
- **MCP 协议** — 连接外部 MCP 服务器作为子进程，自动发现工具并以 `mcp_<server>_<tool>` 命名注册
- **🖼️ 多模态** — 支持图片/音频输入，按媒体类型路由模型（如图片→GPT-4o，文本→DeepSeek）；自动生成媒体文本描述以兼容 Compressor/Extractor；Web UI 提供 `@` 按钮附件上传

## 架构概览

```
                         ┌──────────────────────────────────────┐
                         │          Kugelblitz Agent             │
 用户目标 ──────────────►│                                      │
                         │  ┌────────────────────────────────┐  │
                         │  │  智能路由（Intent 阶段）        │  │
                         │  │  分类 → set_work_mode          │  │
                         │  └──────────────┬─────────────────┘  │
                         │                 │                    │
                         │     simple ─────┴──── plan           │
                         │        │                │            │
                         │        ▼                ▼            │
                         │  ┌──────────┐   ┌───────────────┐   │
                         │  │  Direct  │   │  Agent 状态    │   │
                         │  │ (ReAct)  │   │  Harness (FSM) │   │
                         │  └────┬─────┘   └───────┬───────┘   │
                         │       │                   │           │
                         │       │           ┌───────▼───────┐   │
                         │       │           │  任务编排      │   │
                         │       │           │  (DAG)        │   │
                         │       │           └───────┬───────┘   │
                         │       │                   │           │
                         │       ▼                   ▼           │
                         │  ┌──────────┐   ┌──────────────────┐ │
                         │  │  ReAct   │   │  Plan 引擎       │ │
                         │  │  引擎    │   │  React 执行器    │ │
                         │  │ ReactAgent  │   │  + Reviewer     │ │
                         │  └────┬─────┘   │  + WorkerAgent   │ │
                         │       │         └────────┬─────────┘ │
                         │       │                  │           │
                         │       ▼                  ▼           │
                         │  ┌────────────────────────────────┐  │
                         │  │  记忆系统                      │  │
                         │  │  Session + Working + LTM      │  │
                         │  │  + 自动压缩                    │  │
                         │  └──────────────┬─────────────────┘  │
                         │                 │                    │
                         │  ┌──────────────▼─────────────────┐  │
                         │  │  可观测性                      │  │
                         │  │  OTel Span 层级                 │  │
                         │  │  OTel 全链路追踪         │  │
                         │  │  + 统一 usage 回调             │  │
                         │  └────────────────────────────────┘  │
                         └──────────────────────────────────────┘
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "os/signal"

    "github.com/B777B2056-2/kugelblitz/config"
    "github.com/B777B2056-2/kugelblitz/core"
    "github.com/B777B2056-2/kugelblitz/provider"
    "github.com/B777B2056-2/kugelblitz/runtime"
)

func main() {
    p := provider.DeepSeek("sk-xxx", "https://api.deepseek.com", "deepseek-chat")

    cfg := config.Config{
        Model: config.ModelConfig{
            Provider:        p,
            StreamMode:      true,
            EnableThinking:  true,
            ReasoningEffort: core.ReasoningEffortHigh,
        },
        Runtime:         config.RuntimeConfig{MaxStateMachineCycles: 30},
        ContextCompress: config.ContextCompressConfig{MaxAttempts: 3},
        TargetDrift:     config.TargetDriftConfig{
            ReviewInterval:          12,
            MaxFailuresBeforeReview: 5,
        },
    }

    loop := runtime.NewAgentLoop(cfg)
    loop.RegisterEventHooks(core.AgentEventHooks{
        OnLLMUsage: func(report core.LLMUsageReport) {
            fmt.Printf("[%s] tokens: in=%d out=%d\n",
                report.Identity,
                report.Usage.InputTokens,
                report.Usage.OutputTokens,
            )
        },
    })

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    go func() { <-loop.Done(); cancel() }()
    loop.Run(ctx, "创建 README.md 和 docs/architecture.md")
    <-ctx.Done()
}
```

可选启用 OTel 追踪（在 kugelblitz.yaml 中配置 `otel_*` 键）：

```bash
go run . -apikey sk-xxx
```

## 核心概念

### AgentLoop — 主入口

`runtime.AgentLoop` 是顶层 Harness，单个构造函数完成全栈组装：

```go
loop := runtime.NewAgentLoop(cfg)
loop.RegisterEventHooks(hooks)       // 可选：usage 回调、HITL 等
loop.Run(ctx, "你的目标")            // 异步执行
<-loop.Done()                        // 等待完成
```

内部初始化：长期记忆（MEMORY.md + ChromaDB）、Skills 注册、语义判断器、
会话记忆和 Kernel FSM 引擎。

**生命周期 API**：

| 方法 | 说明 |
|------|------|
| `Run(ctx, goal)` | 启动异步执行 |
| `Done() <-chan struct{}` | 执行完成时关闭 |
| `Cancel()` | 中断主循环 + 取消全部 Worker |
| `HumanLoopWaiting() bool` | 是否正在等待人工输入 |
| `ResumeWithHumanResponse(reply) error` | 注入人工回复，解除阻塞 |

### 智能路由 — 意图识别

创建计划之前，Agent 首先进入 **Intent** 阶段。仅开放 `set_work_mode` 工具，
单次 ReAct 步确定执行路径：

- `{mode: "simple"}` → **Direct** 模式：开放全部工具集（shell、file、web、
  memory、ask_human），直接 ReAct 执行。适用于读文件、运行命令、搜索网页等
  单步任务。
- `{mode: "plan"}` → **Plan** 模式：进入完整 FSM 生命周期（Init → Confirmed
  → Doing）。适用于实现功能、跨文件重构等多步任务。

两种模式均注入相同的 Agent Context 和 Skills。

### Agent 状态 Harness（FSM）

Harness 对每次计划型执行强制施加 9 状态生命周期，每个状态明确定义**可用工具**、
**执行动作**和**转换规则**——LLM 无法跳过或改变流转路径。

详细的状态定义、工具级安全机制和漂移检测闭环见
[Harness — 自愈 & 纠偏](#harness--自愈--纠偏)。

### DAG 任务编排

`Doing` 状态启动后，`DAGTaskExecutor` 接管执行：

- 根据 `ParentTaskID` 计算任务依赖图
- 按**拓扑批次**执行：父任务全部 `done` 的子任务并发运行
- 每个任务由 `WorkerAgent` 独立执行，受限工具集
- 失败触发漂移检测回调
- 批次自动推进，直到全部任务终结或上下文取消

### ReAct 引擎

`infra.ReactAgent` 驱动 think → act → observe 循环：

- **工具白名单** — 每个状态传入允许的工具名列表，Agent 过滤全局注册表
- **流式输出** — 同时支持 Block 和 SSE 流式模式
- **HITL** — 本地 `ask_human` 工具暂停循环，不影响全局注册表
- **中断** — `abortSignal` 通道实现优雅取消

### Worker（执行器）

`infra.WorkerAgent` 执行单个子任务，拥有受限工具集（shell、file、web、
skill_use、ask_human）。由 DAGTaskExecutor 创建，独立任务**并发执行**。
Worker 通过 DAG 共享暂停门支持 HITL。

### 记忆系统

Kugelblitz 提供三层记忆架构：**会话记忆**（短期，对话历史）、**工作记忆**
（进行中的计划与任务）和**长期记忆**（跨会话持久化知识）。

```
┌─────────────────────────────────────────────┐
│              会话记忆                       │  ← 会话隔离，JSONL 持久化
│  消息 + 摘要（自动压缩）                     │
├─────────────────────────────────────────────┤
│              工作记忆                       │  ← 计划 + 任务 + 检查点
│  Plan / Task / Checkpoint（JSONL）          │
├─────────────────────────────────────────────┤
│              长期记忆                       │  ← 持久化，跨会话
│  MEMORY.md + ChromaDB + 知识图谱 + Dreaming  │
└─────────────────────────────────────────────┘
```

#### 会话记忆（Session Memory）

完整对话历史保留在内存中，以 JSONL 格式持久化到磁盘。每次 `AgentLoop` 执行结束
和压缩后自动保存。当上下文窗口溢出时（`ErrContextLengthExceeded`），底座**自动将旧
消息压缩**为 LLM 生成的累积摘要，保留最近 N 条消息。重启时，`SessionMemoryManager`
自动从磁盘恢复。

#### 工作记忆（Working Memory）

记录 Agent 正在执行的内容——**Plan** 及其 **Task**。每次变更都会创建版本化的
**Checkpoint**，支持漂移或失败时回滚。计划以 JSONL 格式通过 `persist` 包持久化。

| 组件 | 说明 |
|------|------|
| `Plan` | 目标拆解，状态生命周期：init → doing → done/failed |
| `Task` | 单个子任务，状态：pending → doing → done/failed |
| `Checkpoint` | 每次变更的版本快照 |

实现见 `memory/working/`。

#### 长期记忆（Long-Term Memory）

所有持久化知识存放在 **MEMORY.md**——唯一权威数据源。
**ChromaDB** 作为语义检索索引，启动时及每次写入后从 MEMORY.md 重建。
查询全部走 ChromaDB；不可用时自动降级为 MEMORY.md 关键词搜索。

**写入 Pipeline**（4 阶段）：

```
会话上下文 + 已有记忆
          │
          ▼
    1. 提取 ── LLM 提取所有记忆类型为 MemoryItem
          │     候选（偏好、事实、情景、经验、模式）
          ▼
    2. 冲突解决 ── 与 MEMORY.md 已有条目对比
          │         • 置信度明确胜出 → 接受
          │         • 置信度接近（差距 < 0.15）→ 提交人工审核
          ▼
    3. 去重 ── 语义去重（对比存量 + 批次内互查）
          │
          ▼
    4. 存储 ── BulkStore → MEMORY.md
                └→ 异步触发 ChromaDB 索引重建
```

**冲突解决策略**：每条新 `MemoryItem` 与已有同 section+key 条目对比。
置信度随时间衰减（`0.95^天数`）。当差距难以判定时，冲突加入待审核队列，
通过 `memory_resolve_conflict` + `ask_human` 由用户确认。

**ChromaDB 索引**：文档以 `mem:{section}:{key}` 为 ID，启动时全量重建
（`RebuildIfStale`），每次写入后异步重建（`Rebuild`）。实现见 `memory/longterm/index.go`。

**Auto Dreaming（后台记忆巩固）**：`DreamScheduler` 后台 goroutine 每 30 分钟轮询一次。
仅在 Agent **空闲**（5 分钟内无 `Execute` 调用）且**冷却期**已过（距上次 dream 超 6 小时）
时才触发 dream 周期。读取已有记忆进行评分，巩固高价值条目，提取跨域洞察写入 `DREAMS.md`。
三阶段：

1. **浅睡（Light Sleep）** — 采集所有 MemoryItem，用图谱度富化
2. **深睡（Deep Sleep）** — LLM 逐条评分（1-10）；高分 → 置信度提升
3. **REM** — LLM 从高分条目中提炼跨域模式 → `insights` section

实现见 `memory/longterm/dream.go`。

**实体关系图谱**：提取管道同时产出实体和关系（`EntityCandidate` / `RelCandidate`），
存入本地内存图（`memory/longterm/graph.go`）并以 JSONL 持久化。自动生成 Mermaid 可视化
文件 `MEMORY_GRAPH.md` 供人类阅读。支持实体搜索、邻居展开、最短路径（BFS）、子图查询。
在 `memory_search` 中设置 `needGraph: true` 即可返回图结果。

**工具列表**：

| 工具 | 说明 |
|------|------|
| `memory_store` | 手动存入一条记忆 |
| `memory_search` | 语义/关键词搜索（走 ChromaDB，fallback MEMORY.md） |
| `memory_get_section` | 列出某 section 全部记忆 |
| `memory_remove` | 删除一条记忆 |
| `memory_list_sections` | 列出所有 section 及数量 |
| `memory_stats` | 统计摘要（总数、section 数、索引状态） |
| `memory_resolve_conflict` | 确认一条待审核冲突（keep_new / keep_old） |
| `memory_extract` | Agent 自主触发：从当前会话提取并持久化 |

#### 工具结果压缩

当工具返回超长字符串值（如 `file_read` 读了 1 万行文件、`web_fetch` 抓了长网页），
底座**自动将该字段用 LLM 压缩**再注入对话上下文——避免浪费 token 和溢出上下文窗口。

- **按字段压缩** — 仅对超过阈值的单个字符串 value 压缩；路径、数字、布尔、短字符串原样保留
- **错误安全** — 包含 `error` 字段的结果整体跳过
- **可配置** — 阈值通过 `ContextCompressConfig.MaxToolResultChars` 设置（默认 4000 字符，0 禁用）

压缩后的值直接替换 Outputs 中的原字段，Agent 看到的是摘要，
而可观测平台中的工具 span 仍保留原始记录。

### Skills（技能模块）

Skills 是可插拔的 YAML/SKILL.md 模块，激活后自动注入系统提示词。
通过 `skill_use` 工具激活，底座自动加载其指令和工具定义。

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
p := provider.DeepSeek("sk-xxx", "https://api.deepseek.com", "deepseek-chat")
agent := infra.NewReactAgent(p, true)

srv := acp.NewServer(agent, p)
srv.Run(context.Background())
```

```bash
go run cmd/acp_server/main.go -apikey sk-xxx
```

编辑器配置示例：

```json
{
  "agent": {
    "command": "go",
    "args": ["run", "cmd/acp_server", "-apikey", "sk-xxx"],
    "work_dir": "/path/to/kugelblitz"
  }
}
```

完整示例见 [cmd/acp_server/](cmd/acp_server/)。

## MCP — Model Context Protocol

Kugelblitz 可连接外部 MCP 服务器，自动发现工具并暴露给 LLM——无需编写任何胶水代码。

### 工作原理

```
MCP 服务器 (子进程)               Kugelblitz (客户端)
     │                                │
     ├─ initialize ──────────────────>│  stdio CommandTransport
     ├─ tools/list ──────────────────>│  发现工具
     │<─ [toolA, toolB, ...] ────────┤
     │                                ├─ 注册为 "mcp_<server>_<tool>"
     │                                │  加入全局 ToolRegistry
     │                                │
     │                         Agent 调用工具
     │                                │
     │<─ tools/call(toolA, args) ─────┤  转发 via session.CallTool
     ├─ result ──────────────────────>│  text / image / error
```

### 配置

在 `kugelblitz.yaml` 的 `mcp_servers` 中添加 MCP 服务器：

```yaml
mcp_servers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_xxx"
  custom:
    command: python3
    args: ["path/to/your/server.py"]
```

各字段说明：

| 字段 | 类型 | 说明 |
|------|------|------|
| `command` | string | 启动可执行文件（必填） |
| `args` | []string | 命令行参数 |
| `env` | map[string]string | 环境变量 |

### 工具命名

发现的工具以 `mcp_<server>_<tool>` 前缀注册，避免与内置工具冲突：

| 服务器 | 原生工具 | 注册名称 |
|--------|---------|---------|
| `github` | `search_repositories` | `mcp_github_search_repositories` |
| `custom` | `echo` | `mcp_custom_echo` |

> **注意**：工具名使用下划线（`_`）作为分隔符——冒号（`:`）不在 DeepSeek 等 LLM
> API 允许的字符集中（`^[a-zA-Z0-9_-]+$`）。

## 人在回路 (Human-in-the-Loop)

底座支持 **agent 主动暂停执行，征询人类意见，待回复后继续**。通过内置的 `ask_human`
工具实现，对 LLM 来说它只是一个普通的工具调用——但执行时会阻塞整个 ReAct 循环直到
人类回复。

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
  ├── 思考 → 调用 tool_C（根据回复继续执行）
  ├── 思考 → ...（ReAct 循环恢复正常）
  └── 最终结果
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

- **对 LLM 透明** — `ask_human` 和普通工具完全一致：有 tool definition、有 tool result
- **`ToolCallResult` 零改动** — 暂停机制完全封装在工具执行内部
- **`Interrupt` 零改动** — `Interrupt()` 只管理 `abortSignal`；暂停通过 context 取消解除
- **本地工具注册** — `ask_human` 不在全局注册表，每个 agent 实例独立注册
- **默认启用** — `NewKernel` 和 `NewWorkerAgent` 自动启用 HITL
- **DAG 全局暂停** — Worker 在 DAG 执行中进入 HITL 时，**同 DAG 内所有 Worker
  均通过共享暂停门同步暂停**。这避免了人工回复期间其他任务继续执行导致计划状态不一致。
  收到人工回复后，所有 Worker 统一恢复

### 示例

```go
agent := infra.NewReactAgent(p, true)
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

## Agent 上下文文件

底座启动时会从 `~/.kugelblitz/` 目录自动加载以下文件，并注入 System Prompt：

| 文件 | 用途 |
|------|------|
| `AGENTS.md` | Agent 能力声明 |
| `IDENTITY.md` | Agent 身份定义 |
| `SOUL.md` | Agent 性格/语气 |
| `USER.md` | 用户偏好/档案 |

缺失或为空的文件会被静默跳过。这是一种**零代码的 Agent 定制方式**——将文件放入
workspace 目录即可改变 agent 行为。

## Harness — 自愈 & 纠偏

底座内置了**无需人工介入**的安全机制，确保执行始终围绕目标。

### Agent 状态 Harness

Harness 通过**有限状态机强制约束** Agent 生命周期——LLM 无法跳过或改变流转路径：

```
                 ┌──────────┐
                 │  Intent  │  意图识别
                 └────┬─────┘
           simple ───┴─── plan
              │              │
              ▼              ▼
         ┌────────┐    ┌──────────┐
         │ Direct │    │   Init   │  创建计划
         └────────┘    └────┬─────┘
                            │ plan valid
                            ▼
                      ┌───────────┐
                      │ Confirmed │  用户确认
                      └──┬───┬────┘
                         │   │ rejected
                  approved│   ▼
                         │  ┌──────────┐
                         │  │ Rejected │
                         │  └──────────┘
                         ▼
                      ┌──────────┐
                 ┌───►│  Doing   │  DAG 并行执行
                 │    └────┬─────┘
                 │         │         all done
                 │         │              │
                 │         │              ▼
                 │         │         ┌──────────┐
                 │         │         │   Done   │  汇总
                 │         │         └──────────┘
                 │         │
                 │   目标发生漂移
                 │         │
                 │         ▼
                 │   ┌──────────┐
                 │   │ Updating │  重规划
                 │   └────┬─────┘
                 │        │
                 │   plan valid
                 └────────┘
```

每次计划型执行经过 9 状态 FSM 生命周期。每个状态由三个明确要素定义：

| 要素 | 说明 |
|------|------|
| **工具白名单** | LLM 在该状态下可调用的工具 |
| **动作（Action）** | 执行的原子操作（ReactAction / DAGAction / NoOpAction） |
| **转换规则** | 满足条件时切换到哪个状态 |

各状态与工具：

| 状态 | 工具 | 动作 |
|------|------|------|
| `Intent` | `set_work_mode` | ReactAction → 分类 |
| `Init` | `plan_create`、`task_insert`、`memory_*`、`skill_use` | ReactAction → 构建计划 |
| `Confirmed` | `ask_human`、`confirm_plan` | ReactAction → 等待确认 |
| `Doing` | `task_query`、`task_status_update` | DAGAction → 执行任务 |
| `Updating` | `task_*`、`plan_query`、`memory_*`、`skill_use` | ReactAction → 重规划 |
| `Done` | `task_query`、`plan_query` | ReactAction → 汇总 |
| `Failed` | `task_query`、`plan_query` | ReactAction → 失败总结 |
| `Direct` | shell、file、web、memory、`skill_use`、`ask_human` | ReactAction → 执行 |
| `Rejected` | 无 | NoOpAction → 终止 |

状态流转由 **Harness 强制执行**，LLM 无法跳过或改变流转路径。这保证了 Agent 行为
每一步都可预测、可审计。

### Tool 调用 Harness

每个 tool 调用都经过 Harness 的两层安全防护，在进入 LLM 上下文之前完成拦截：

**按状态的 tool 可见性** — FSM 状态通过**白名单**决定 LLM 能看见哪些 tool。
不在名单上的 tool 对模型完全不可见，避免模型幻觉误调用导致状态机不稳定：

| 状态 | 可见 tool | 控制意图 |
|------|----------|---------|
| `Intent` | 仅 `set_work_mode` | 分类完成前禁止调用文件/Shell 等执行类 tool |
| `Init` | plan 类 + memory，**不含 ask_human** | 强制先产出有效计划，不能跳过直接问人 |
| `Confirmed` | 仅 `ask_human` + `confirm_plan` | 等待用户决策期间禁止修改计划 |
| `Doing` | 仅 `task_query` + `task_status_update` | 执行中禁止创建新任务，防止计划膨胀 |
| 终端状态 | 仅查询类 tool | 可查看不可修改 |

**入参校验** — 每个内置 tool 在 Harness 层完成参数验证：
- 必填字段检查存在性与类型
- 字符串字段长度截断（避免无限输入）
- 枚举值约束在允许集合内
- 错误结果短路处理，不进入 LLM 对话上下文

### 执行中漂移检测

`Doing` 状态期间，两种条件独立触发 Reviewer：

1. **步数间隔** — 每 N 个 ReAct 步审查一次（`ReviewInterval`），不论成功与否
2. **连续失败** — 任务连续失败 N 次时触发（`MaxFailuresBeforeReview`）

Reviewer 将**原始目标**、**当前计划状态**和**近期活动**发给 LLM，
通过 `reviewer_report` 工具返回结构化判断：

```go
type ReviewResult struct {
    Drift      bool        // 是否偏离目标
    Reason     string      // 偏离原因
    Suggestion string      // 建议修正措施
}
```

若 `Drift == true`，底座自动回滚到上一 checkpoint，迁入 `Updating`（重规划），
再回到 `Confirmed` → `Doing`。同时向 session 注入 `system` 消息，触发
`OnPlanRollback` 回调，前端可据此渲染回滚通知。

这确保 Agent 始终聚焦**原始目标**，避免在无关或范围蔓延的任务上浪费 token。

## 可观测性

### OpenTelemetry 全链路追踪

执行过程自动生成完整层级：

```
Trace "planner: <目标>"
  ├── Span "react.step" #1
  │   ├── Span "step-1-llm"          ← 输入/输出 token
  │   ├── Span "tool:plan_create"    ← 入参 + 执行结果
  │   └── Span "tool:task_insert"
  ├── Span "reviewer.check"          ← 漂移评估 + token 用量
  ├── Span "compress.summarize"      ← 压缩摘要 + token 用量
  ├── Span "react.step" #2
  │   ├── Span "step-2-llm"
  │   └── Span "tool:worker_spawn"
  └── Span "memory.extract_before_compress"
```

通过 `kugelblitz.yaml` 配置即可接入：

```yaml
otel_enabled: true
otel_endpoint: "https://api.langfuse.com/api/public/otel/v1/traces"
otel_auth_header: "<base64(pk:sk)>"
otel_service_name: "kugelblitz"
```

或在代码中：

```go
shutdown, _ := observability.InitTracer(ctx, cfg.Observability)
defer shutdown()
```

### 统一 Usage 回调

单一回调即可获取**全部 LLM 调用**的 token 消耗，按来源区分：

```go
loop.RegisterEventHooks(core.AgentEventHooks{
    OnLLMUsage: func(report core.LLMUsageReport) {
        fmt.Printf("[%s] in=%d out=%d total=%d\n",
            report.Identity,
            report.Usage.InputTokens,
            report.Usage.OutputTokens,
            report.Usage.TotalTokens,
        )
    },
})
```

回调中的 Identity 标识：`planner.step-1`、`planner.step-2`、`compressor`、
`reviewer`、`worker.<task-id>`。

## 内置工具

### 意图与路由

| 工具 | 说明 |
|------|------|
| `set_work_mode` | 结构化分类：分析请求 → 选择模式（"plan" 或 "simple"） |

### 计划与任务

| 工具 | 说明 |
|------|------|
| `plan_create` | 创建新的空计划 |
| `plan_query` | 按 ID 查询计划或列出全部 |
| `confirm_plan` | 用户审查后确认或拒绝计划 |
| `plan_rollback` | 回滚计划到之前的检查点 |
| `task_insert` | 向计划插入子任务 |
| `task_delete` | 删除任务 |
| `task_query` | 按 ID 查询任务或列出计划内任务 |
| `task_status_update` | 更新任务状态（pending → doing → done / failed） |

### 记忆

| 工具 | 说明 |
|------|------|
| `memory_store` | 手动存入一条记忆 |
| `memory_search` | 语义/关键词搜索（ChromaDB，fallback MEMORY.md），支持 `needGraph` |
| `memory_get_section` | 列出某 section 全部记忆 |
| `memory_remove` | 删除一条记忆 |
| `memory_list_sections` | 列出所有 section 及数量 |
| `memory_stats` | 统计：总数、section 数、索引状态 |
| `memory_resolve_conflict` | 确认一条待审核冲突（keep_new / keep_old） |
| `memory_extract` | Agent 自主触发：从当前会话提取并持久化 |

### 文件与 Shell

| 工具 | 说明 |
|------|------|
| `file_read` | 读取文件 |
| `file_write` | 写入/创建文件 |
| `file_delete` | 删除文件 |
| `file_copy` | 复制或移动文件 |
| `dir_create` | 创建目录 |
| `dir_copy` | 复制或移动目录 |
| `shell_exec` | 执行 Shell 命令 |

### 网络

| 工具 | 说明 |
|------|------|
| `web_search` | 搜索网页（默认 DuckDuckGo） |
| `web_fetch` | 抓取网页并转换为 Markdown |

### 交互

| 工具 | 说明 |
|------|------|
| `skill_use` | 按名称激活技能 |
| `ask_human` | 暂停并征询人类用户输入 |

## 目录结构

```
kugelblitz/
├── core/              # 接口定义：ILMProvider, Observer, Span, Message, Tool, IAgent
├── config/            # 配置结构体（Model, Runtime, Compress, Drift）
├── constants/         # 枚举：PlanState, RoleType, MultiModalType
├── runtime/           # Agent 运行时
│   ├── agent_loop.go  #   AgentLoop — 主入口
│   └── engine/
│       ├── kernel.go  #   Kernel — 公共 API 门面
│       ├── fsm/       #   状态机（State + Action + Machine）
│       ├── dag/       #   DAG 任务执行器（拓扑批次并发）
│       └── infra/     #   基础设施（ReactAgent, Reviewer, WorkerAgent）
├── memory/
│   ├── session_memory.go  # SessionMemory — 对话历史 + 自动压缩
│   ├── compressor.go      # LLM 上下文压缩
│   ├── working/           # 工作记忆（Plan + Task + Checkpoint）
│   └── longterm/          # 长期记忆（MEMORY.md + ChromaDB + Graph + Dream）
├── prompts/           # 系统提示词模板
├── observability/     # OTel Span 层级 + OTel SDK, PlannerInstrument
├── tools/
│   ├── mcp/           # MCP 服务集成（客户端、管理器、工具注册）
│   └── internals/     # 内置工具（plan_*, task_*, memory_*, web, file, shell）
├── skills/            # Skill 加载与注册
├── provider/
│   └── chat_completions/  # OpenAI 兼容 Format（Block + Stream）
├── persist/           # 格式级存储：MarkdownPersist, JSONLPersist, VectorPersist
├── utils/             # UUID 生成, session ID
└── cmd/
    ├── common/        #   共享 YAML 配置工具
    ├── kugelblitz-ui/ #   Web UI 服务器（HTTP + SSE 流式）
    └── acp_server/    #   ACP 服务端（编辑器兼容 Agent）
```

### 构建二进制文件

```bash
# 构建所有 cmd 二进制 → bin/
make build

# 构建指定二进制
make build build-cmds=kugelblitz-ui

# 构建并安装到 $GOPATH/bin
make install
make install build-cmds=kugelblitz-ui PREFIX=/usr/local

# 启动 Web UI（默认 :8088）
./bin/kugelblitz-ui
./bin/kugelblitz-ui -addr :9090

# 启动 ACP 服务端
./bin/acp_server -v
```

### 工作目录布局（`~/.kugelblitz/`）

```
~/.kugelblitz/
├── MEMORY.md                          # 长期记忆（权威源，人类可编辑）
├── DREAMS.md                           # 梦境日记（自动生成的反思日志）
├── AGENTS.md                          # Agent 能力描述（只读）
├── IDENTITY.md                        # Agent 身份（只读）
├── SOUL.md                            # Agent 个性（只读）
├── USER.md                            # 用户画像（只读）
├── kugelblitz.yaml                    # 主配置（包含 mcp_servers）
├── skills/
│   └── {name}/SKILL.md                # 技能定义（只读）
│
└── memory/                            # Agent 托管数据
    ├── sessions/{id}.jsonl            # 会话记忆（JSONL）
    ├── plans/{planID}/
    │   ├── plan.jsonl                 # 工作记忆 — Plan
    │   └── checkpoints/{v}.jsonl      # Plan 版本快照
    └── longterm/
        ├── memory_graph.jsonl         # 实体关系图谱（JSONL）
        └── MEMORY_GRAPH.md            # 实体关系图谱（Mermaid，只读）
```
