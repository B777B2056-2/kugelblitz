# Kugelblitz

[中文](README_zh.md)

A lightweight, modular Go harness agent — smart routing, FSM state harness, and DAG
task orchestration that keep LLM agents predictable, auditable, and controllable.

## Features

- **🧭 Smart Routing** — single structured LLM call classifies intent: simple task → Direct (zero-planning), complex task → Plan (full FSM lifecycle)
- **⚙️ Agent State Harness** — FSM-enforced 9-state lifecycle, predictable and auditable. See [Harness — Self-Healing & Drift Prevention](#harness--self-healing--drift-prevention)
- **📋 DAG Task Orchestration** — topological batching, concurrent execution, failure isolation + drift callback
- **ReAct Engine** — think → act → observe loop with streaming, tool whitelists per state
- **Three-Layer Memory** — Session (auto-compress) + Working (Plan/Task/Checkpoint) + Long-Term (MEMORY.md + ChromaDB + Knowledge Graph + auto Dreaming)
- **Goal-Drift Review** — periodic alignment check with automatic checkpoint rollback
- **Human-in-the-Loop** — `ask_human` tool pauses execution; DAG-wide pause gate; `OnWaitForHumanAction` + `ResumeWithHumanResponse` resume
- **Skills** — pluggable YAML/SKILL.md domain-knowledge modules
- **Built-in Web Tools** — `web_search` (DuckDuckGo, zero-config) + `web_fetch` (HTML→Markdown, optional JS rendering)
- **Observability** — OpenTelemetry tracing with full trace/span/generation hierarchy
- **Unified Usage Callback** — single callback for all LLM token consumption, tagged by source identity
- **ACP (Agent Client Protocol)** — JSON-RPC 2.0 over stdio, compatible with Zed / JetBrains / VS Code / Neovim
- **MCP (Model Context Protocol)** — connect external MCP servers as subprocesses, auto-discover and register their tools with `mcp_<server>_<tool>` naming
- **🖼️ Multimodal** — image/audio input support with per-type model routing (e.g. GPT-4o for images, DeepSeek for text). Auto-describe media for Compressor/Extractor compatibility. Web UI `@` button for file attachment.

## Architecture

```
                         ┌──────────────────────────────────────┐
                         │          Kugelblitz Agent             │
 User Goal ─────────────►│                                      │
                         │  ┌────────────────────────────────┐  │
                         │  │  Smart Routing (Intent phase)  │  │
                         │  │  classify → set_work_mode      │  │
                         │  └──────────────┬─────────────────┘  │
                         │                 │                    │
                         │     simple ─────┴──── plan           │
                         │        │                │            │
                         │        ▼                ▼            │
                         │  ┌──────────┐   ┌───────────────┐   │
                         │  │  Direct  │   │  Agent State   │   │
                         │  │ (ReAct)  │   │  Harness (FSM) │   │
                         │  └────┬─────┘   └───────┬───────┘   │
                         │       │                   │           │
                         │       │           ┌───────▼───────┐   │
                         │       │           │  Task         │   │
                         │       │           │  Orchestration│   │
                         │       │           │  (DAG)        │   │
                         │       │           └───────┬───────┘   │
                         │       │                   │           │
                         │       ▼                   ▼           │
                         │  ┌──────────┐   ┌──────────────────┐ │
                         │  │  ReAct   │   │  Plan Engine     │ │
                         │  │  Engine  │   │  React Executor  │ │
                         │  │ ReactAgent  │   │  + Reviewer     │ │
                         │  └────┬─────┘   │  + WorkerAgent   │ │
                         │       │         └────────┬─────────┘ │
                         │       │                  │           │
                         │       ▼                  ▼           │
                         │  ┌────────────────────────────────┐  │
                         │  │  Memory System                │  │
                         │  │  Session + Working + LTM      │  │
                         │  │  + auto-compress              │  │
                         │  └──────────────┬─────────────────┘  │
                         │                 │                    │
                         │  ┌──────────────▼─────────────────┐  │
                         │  │  Observability                 │  │
                         │  │  OTel Span hierarchy            │  │
                         │  │  OTel tracing     │  │
                         │  │  + unified usage callback      │  │
                         │  └────────────────────────────────┘  │
                         └──────────────────────────────────────┘
```

## Quick Start

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
    loop.Run(ctx, "Create a README.md and docs/architecture.md")
    <-ctx.Done()
}
```

Run with optional OTel tracing (configure via `otel_*` keys in kugelblitz.yaml):

```bash
go run . -apikey sk-xxx
```

## Core Concepts

### AgentLoop — Main Entry Point

`runtime.AgentLoop` is the top-level harness. A single constructor wires up the
entire stack:

```go
loop := runtime.NewAgentLoop(cfg)
loop.RegisterEventHooks(hooks)       // optional: usage callbacks, HITL, etc.
loop.Run(ctx, "your goal here")      // async execution
<-loop.Done()                        // wait for completion
```

Under the hood it initializes: Long-Term Memory (MEMORY.md + ChromaDB), Skills
registry, Semantic Judge, Session Memory, and the Kernel FSM engine.

**Lifecycle API**:

| Method | Description |
|--------|-------------|
| `Run(ctx, goal)` | Start async execution |
| `Done() <-chan struct{}` | Closed when execution completes |
| `Cancel()` | Interrupt main loop + cancel all workers |
| `HumanLoopWaiting() bool` | Whether agent is waiting for human input |
| `ResumeWithHumanResponse(reply) error` | Unblock HITL with human response |

### Smart Routing — Intent Recognition

Before any plan is created, the agent enters the **Intent** state. A single ReAct
step with only the `set_work_mode` tool determines the execution path:

- `{mode: "simple"}` → **Direct** mode: runs a ReAct loop with the full tool set
  (shell, file, web, memory, ask_human). Suitable for single-step tasks like
  reading files, running commands, or searching the web.
- `{mode: "plan"}` → **Plan** mode: enters the FSM lifecycle (Init → Confirmed →
  Doing). Suitable for multi-step tasks like implementing features or refactoring.

Both modes receive the same Agent Context and Skills injection.

### Agent State Harness (FSM)

The harness enforces a 9-state lifecycle over every plan-based execution.
Each state defines **allowed tools**, **actions**, and **transition rules** —
the LLM cannot skip or alter the flow path.

See [Harness — Agent State Harness & Tool Harness](#harness--self-healing--drift-prevention)
for detailed state definitions, tool-level safety mechanisms, and the drift detection loop.

### DAG Task Orchestration

When `Doing` begins, `DAGTaskExecutor` takes over:

- Computes task dependency graph from `ParentTaskID` fields
- Executes in **topological batches**: all tasks whose parents are `done` run concurrently
- Each task gets a `WorkerAgent` with a restricted tool set
- Failures trigger the drift detection callback
- Batches auto-advance until all tasks terminal or context cancelled

### ReAct Engine

`infra.ReactAgent` powers the think→act→observe loop:

- **Tool whitelists** — each state passes a list of allowed tool names; the agent
  filters the global registry to expose only those tools
- **Streaming** — supports both block and SSE streaming modes
- **HITL** — local `ask_human` tool pauses the loop without affecting the global registry
- **Interrupt** — `abortSignal` channel for graceful cancellation

### Worker

`infra.WorkerAgent` executes individual tasks with a restricted tool set (shell,
file, web, skill_use, ask_human). Spawned by DAGTaskExecutor, workers run
concurrently when tasks are independent. Each worker exposes HITL support via
the DAG's shared pause gate.

### Memory System

Kugelblitz provides a three-layer memory architecture: **Session Memory** (short-term,
conversation history), **Working Memory** (plans and tasks in progress), and
**Long-Term Memory** (persistent knowledge across sessions).

```
┌─────────────────────────────────────────────┐
│              Session Memory                 │  ← session-isolated, JSONL persisted
│  Messages + Summary (auto-compressed)       │
├─────────────────────────────────────────────┤
│              Working Memory                 │  ← plans + tasks + checkpoints
│  Plan / Task / Checkpoint (JSONL)           │
├─────────────────────────────────────────────┤
│              Long-Term Memory               │  ← persistent, cross-session
│  MEMORY.md + ChromaDB + Graph + Dreaming     │
└─────────────────────────────────────────────┘
```

#### Session Memory

Full conversation history kept in memory. Persisted as JSONL to disk after every
`AgentLoop` execution and after compression. When the context window overflows
(`ErrContextLengthExceeded`), the harness **automatically compresses** old messages
into an LLM-generated cumulative summary, keeping the most recent N messages intact.
On restart, `SessionMemoryManager` automatically reloads from disk.

#### Working Memory

Tracks what the agent is currently doing — the **Plan** and its **Tasks**.
Every mutation creates a versioned **Checkpoint**, enabling rollback on drift or
failure. Plans are persisted as JSONL via the `persist` package.

| Component | Description |
|-----------|-------------|
| `Plan` | Goal decomposition with status lifecycle (init → doing → done/failed) |
| `Task` | Individual subtask with status (pending → doing → done/failed) |
| `Checkpoint` | Versioned snapshot saved on every mutation |

See `memory/working/` for the implementation.

#### Long-Term Memory

All persistent knowledge lives in **MEMORY.md** — the single authoritative source.
**ChromaDB** acts as a semantic search index, rebuilt from MEMORY.md at startup
and after every write. Queries go through ChromaDB; if unavailable, they fall back
to keyword search on MEMORY.md.

**Write Pipeline** (4 stages):

```
Session Context + Existing Memories
          │
          ▼
    1. Extract  ── LLM extracts all memory types as MemoryItem
          │         candidates (facts, episodic, lessons, patterns)
          ▼
    2. Resolve  ── Compare against existing MEMORY.md entries
          │         • Clear winner → accept
          │         • Close confidence (< 0.15 gap) → defer to human
          ▼
    3. Dedup    ── Semantic dedup vs. existing + batch peers
          │
          ▼
    4. Store    ── BulkStore → MEMORY.md
                   └→ async trigger ChromaDB index rebuild
```

**Conflict Resolution**: each new `MemoryItem` is compared against the existing
entry with the same section+key. Confidence decays over time (`0.95^days`).
When confidence gap is narrow, the conflict is queued for human review via
`memory_resolve_conflict` + `ask_human`.

**ChromaDB Index**: documents use key `mem:{section}:{key}`, rebuilt from
MEMORY.md at startup (`RebuildIfStale`) and async after every write (`Rebuild`).
See `memory/longterm/index.go`.

**Dreaming** (background consolidation): a `DreamScheduler` runs on a background
goroutine, polling every 30 minutes. It only triggers a dream cycle when the
agent has been **idle** (no `Execute` calls for 5 minutes) and the **cooldown**
has elapsed (6 hours since last dream). The cycle reads existing memories,
scores them by value, consolidates high-value items, and extracts cross-cutting
insights. Results are written to `DREAMS.md`. Three phases:

1. **Light Sleep** — collect all `MemoryItem`s, enrich with graph degree
2. **Deep Sleep** — LLM scores each item (1–10); high scores get confidence bump
3. **REM** — LLM extracts patterns and themes from top items → `insights` section

See `memory/longterm/dream.go`.

**Entity-Relationship Graph**: the extraction pipeline also produces entities
and relationships (`EntityCandidate` / `RelCandidate`), stored in a local
in-memory graph (`memory/longterm/graph.go`) and persisted as JSONL. A
human-readable Mermaid visualization is auto-generated at `MEMORY_GRAPH.md`.
Supports entity search, neighbor expansion, shortest-path (BFS), and subgraph
queries. Set `needGraph: true` on `memory_search` to include graph results.

**Tools**:

| Tool | Description |
|------|-------------|
| `memory_store` | Manually store a memory item |
| `memory_search` | Semantic/keyword search via ChromaDB (fallback: MEMORY.md) |
| `memory_get_section` | List all items in a section |
| `memory_remove` | Delete an item |
| `memory_list_sections` | List all sections with counts |
| `memory_stats` | Stats: total items, sections, index status |
| `memory_resolve_conflict` | Confirm a pending conflict (keep_new / keep_old) |
| `memory_extract` | Agent-triggered: extract + persist from current session |

#### Tool Result Compression

When a tool returns a large string value (e.g. `file_read` of a 10000-line file,
or `web_fetch` of a long page), the harness **automatically compresses** that field
via the LLM before it enters the conversation context — preventing token waste and
context overflow.

- **Per-field** — only individual string values that exceed the threshold are
  compressed; other fields (paths, numbers, booleans, short strings) stay intact.
- **Error-safe** — results containing an `error` key are never compressed.
- **Configurable** — threshold via `ContextCompressConfig.MaxToolResultChars` (default 4000 UTF-8 chars, 0 = disable).

Compressed fields are replaced in-place within `Outputs`, so the agent sees
the summarized version while the tool span in the observability backend still records the original.

### Skills

Skills are pluggable YAML/SKILL.md modules that inject domain knowledge into the
system prompt. Activate a skill via `skill_use` and the harness automatically
loads its instructions and tool definitions.

## ACP — Agent Client Protocol

Kugelblitz can run as an ACP-compatible agent for any editor that supports the
[Agent Client Protocol](https://agentclientprotocol.com) (Zed, JetBrains,
VS Code, Neovim).

### Protocol Flow

```
Editor (Client)              Kugelblitz (Server)
     │                            │
     ├─ initialize ──────────────>│  version negotiation + capabilities
     │<─ initialize result ──────┤
     ├─ session/new ─────────────>│  create session (cwd)
     │<─ session/new result ─────┤  (returns sessionId)
     ├─ session/prompt ──────────>│  user message
     │<─ session/update ──────────┤  streaming text chunks
     │<─ session/update ──────────┤  tool calls + results
     │<─ session/prompt result ───┤  end_turn / cancelled
     ├─ session/cancel ──────────>│  interrupt
     ├─ session/load ────────────>│  resume historical session
     ├─ session/list ────────────>│  list all sessions
```

### Supported Methods

| Method | Dir | Description |
|--------|-----|-------------|
| `initialize` | C→A | Protocol version + capability negotiation |
| `session/new` | C→A | Create session with working directory |
| `session/prompt` | C→A | Send user prompt |
| `session/cancel` | C→A | Cancel active prompt execution |
| `session/load` | C→A | Load + replay history of existing session |
| `session/list` | C→A | List all active sessions |
| `session/delete` | C→A | Delete a session |
| `session/update` | A→C | Streaming notifications (text chunks, tool calls) |

### Quick Start

```go
p := provider.DeepSeek("sk-xxx", "https://api.deepseek.com", "deepseek-chat")
agent := infra.NewReactAgent(p, true)

srv := acp.NewServer(agent, p)
srv.Run(context.Background())
```

```bash
go run cmd/acp_server/main.go -apikey sk-xxx
```

Editor configuration example:

```json
{
  "agent": {
    "command": "go",
    "args": ["run", "cmd/acp_server", "-apikey", "sk-xxx"],
    "work_dir": "/path/to/kugelblitz"
  }
}
```

Full example at [cmd/acp_server/](cmd/acp_server/).

## MCP — Model Context Protocol

Kugelblitz can connect to external MCP servers, discover their tools, and expose
them to the LLM — all without writing any glue code.

### How It Works

```
MCP Server (subprocess)          Kugelblitz (client)
     │                                │
     ├─ initialize ──────────────────>│  stdio CommandTransport
     ├─ tools/list ──────────────────>│  discover tools
     │<─ [toolA, toolB, ...] ────────┤
     │                                ├─ register as "mcp_<server>_<tool>"
     │                                │  in global ToolRegistry
     │                                │
     │                         Agent calls tool
     │                                │
     │<─ tools/call(toolA, args) ─────┤  forward via session.CallTool
     ├─ result ──────────────────────>│  text / image / error
```

### Configuration

Add MCP servers in `kugelblitz.yaml` under the `mcp_servers` key:

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

Each server definition:

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | Executable to launch (required) |
| `args` | []string | Command arguments |
| `env` | map[string]string | Environment variables |

### Tool Naming

Discovered tools are registered with a `mcp_<server>_<tool>` prefix to avoid
collisions with built-in tools:

| Server | Native Tool | Registered Name |
|--------|-------------|-----------------|
| `github` | `search_repositories` | `mcp_github_search_repositories` |
| `custom` | `echo` | `mcp_custom_echo` |

> **Note**: Tool names use underscores (`_`) as separators — colons (`:`) are
> not allowed by DeepSeek and other LLM APIs (`^[a-zA-Z0-9_-]+$`).

## Human-in-the-Loop

The harness supports **agent-initiated pause for human consultation** — the agent
calls the built-in `ask_human` tool, which blocks the ReAct loop until a human
responds via `ResumeWithHumanResponse`.

### How It Works

```
ReAct Loop
  │
  ├── think → call tool A
  ├── think → call tool B
  ├── think → call ask_human(question="Delete this file?", reason="need approval")
  │           │
  │           ├─ fires OnWaitForHumanAction callback  ← notify external system
  │           ├─ blocks waiting for human...           ← pause point
  │           │     ↑
  │           │     │ ResumeWithHumanResponse(ctx, "yes, delete")
  │           │
  │           └─ returns {"response":"yes, delete"} to LLM
  │
  ├── think → call tool C (acts on human response)
  ├── think → ... (ReAct loop resumes normally)
  └── final result
```

### Core API

| API | Description |
|------|-------------|
| `core.HumanGate` | `WaitForHuman(ctx, reason, prompt) (string, error)` — interface tools call to pause |
| `OnWaitForHumanAction(reason, prompt)` | `AgentEventHooks` callback fired when agent enters waiting state |
| `agent.EnableHumanInTheLoop()` | Enables HITL, registers `ask_human` as a local tool |
| `agent.HumanLoopWaiting() bool` | Reports whether agent is currently waiting |
| `agent.ResumeWithHumanResponse(ctx, reply)` | Injects human response to unblock |

### Design Decisions

- **Zero changes to `ToolCallResult`** — pause mechanism is entirely encapsulated within the tool's `Execute`
- **Zero changes to `Interrupt`** — `Interrupt()` only manages `abortSignal`; pause is cancelled via context
- **Local tool registration** — `ask_human` is registered per-agent, not globally, holding a reference to the agent's `HumanGate`
- **Enabled by default** — `NewKernel` and `NewWorkerAgent` enable HITL automatically
- **DAG-wide pause** — when any Worker enters HITL during DAG execution, **all Workers in the
  same DAG are paused** via a shared pause gate. This prevents sibling tasks from racing ahead
  while the human provides input on the blocked task, ensuring consistent plan state.

### Example

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

## Agent Context Files

On startup, the harness auto-loads files from `~/.kugelblitz/` and injects them
into the system prompt:

| File | Purpose |
|------|---------|
| `AGENTS.md` | Agent capabilities declaration |
| `IDENTITY.md` | Agent identity definition |
| `SOUL.md` | Agent personality / tone |
| `USER.md` | User preferences / profile |

Missing or empty files are silently skipped. This is **zero-code agent customization** —
drop files into the workspace directory to change agent behavior.

## Harness — Self-Healing & Drift Prevention

The harness provides built-in safety mechanisms that operate **without human
intervention** to keep execution on track.

### Agent State Harness

The harness **enforces** a finite state machine over the agent's lifecycle — the LLM
cannot skip or alter the flow:

```
                 ┌──────────┐
                 │  Intent  │  Intent recognition
                 └────┬─────┘
           simple ───┴─── plan
              │              │
              ▼              ▼
         ┌────────┐    ┌──────────┐
         │ Direct │    │   Init   │  Create plan
         └────────┘    └────┬─────┘
                            │ plan valid
                            ▼
                      ┌───────────┐
                      │ Confirmed │  User approval
                      └──┬───┬────┘
                         │   │ rejected
                  approved│   ▼
                         │  ┌──────────┐
                         │  │ Rejected │
                         │  └──────────┘
                         ▼
                      ┌──────────┐
                 ┌───►│  Doing   │  DAG parallel execution
                 │    └────┬─────┘
                 │         │         all done
                 │         │              │
                 │         │              ▼
                 │         │         ┌──────────┐
                 │         │         │   Done   │  Summarize
                 │         │         └──────────┘
                 │         │
                 │    goal drift detected
                 │         │
                 │         ▼
                 │   ┌──────────┐
                 │   │ Updating │  Replan
                 │   └────┬─────┘
                 │        │
                 │   plan valid
                 └────────┘
```

Every plan-based execution goes through a 9-state FSM lifecycle. Each state is
defined by three explicit properties:

| Property | Description |
|----------|-------------|
| **Tool whitelist** | Which tools the LLM can call in this state |
| **Action** | The atomic operation performed (ReactAction / DAGAction / NoOpAction) |
| **Transition rule** | Under which conditions the state changes, and to what |

States and their tools:

| State | Tools | Action |
|-------|-------|--------|
| `Intent` | `set_work_mode` | ReactAction → classify |
| `Init` | `plan_create`, `task_insert`, `memory_*`, `skill_use` | ReactAction → build plan |
| `Confirmed` | `ask_human`, `confirm_plan` | ReactAction → await approval |
| `Doing` | `task_query`, `task_status_update` | DAGAction → execute tasks |
| `Updating` | `task_*`, `plan_query`, `memory_*`, `skill_use` | ReactAction → replan |
| `Done` | `task_query`, `plan_query` | ReactAction → summarize |
| `Failed` | `task_query`, `plan_query` | ReactAction → failure summary |
| `Direct` | shell, file, web, memory, `skill_use`, `ask_human` | ReactAction → execute |
| `Rejected` | (none) | NoOpAction → terminal |

Transitions are **enforced by the harness** — the LLM cannot skip or alter the
flow. This guarantees predictable, auditable agent behavior at every step.

### Tool Harness

Every tool invocation passes through the harness, which applies two layers of
safety before the tool ever reaches the LLM's context:

**Per-state tool visibility** — The FSM state determines exactly which tools are
exposed to the LLM via a **whitelist**. Tools not on the list are invisible to the
model, preventing hallucinated tool calls that could destabilize the state machine:

| State | Tools visible | Why |
|-------|---------------|-----|
| `Intent` | only `set_work_mode` | LLM cannot call file/shell tools before classification |
| `Init` | plan tools + memory, **no ask_human** | LLM must create a valid plan before seeking approval |
| `Confirmed` | only `ask_human` + `confirm_plan` | LLM cannot modify the plan while awaiting user decision |
| `Doing` | only `task_query` + `task_status_update` | LLM cannot create new tasks mid-execution |
| Terminal | query-only tools | LLM can inspect but not modify |

**Input validation** — Each built-in tool validates its arguments at the harness
level before execution:
- Required fields are checked for presence and type
- String fields are bounded (no unlimited-length inputs)
- Enumerated values are validated against allowed sets
- Error results short-circuit the tool pipeline and never enter the LLM context

### Drift Detection During Execution

Two triggers independently activate the Reviewer during the `Doing` state:

1. **Step interval** — every N ReAct steps (`ReviewInterval`), regardless of
   success or failure.
2. **Consecutive failures** — after N consecutive task failures
   (`MaxFailuresBeforeReview`).

The Reviewer sends the **original goal**, **current plan state**, and **recent
activity** to an LLM using a `reviewer_report` tool for structured output:

```go
type ReviewResult struct {
    Drift      bool        // true if execution has deviated
    Reason     string      // explanation
    Suggestion string      // suggested corrective action
}
```

If `Drift == true`, the harness auto-rolls back to the previous checkpoint,
transitions to `Updating` (replan), then re-enters `Confirmed` → `Doing`.
A `system` message is injected so the LLM explains the rollback in its next
response. The `OnPlanRollback` callback also fires for frontend notification.

This keeps execution focused on the **original goal** and prevents token waste
on irrelevant or scope-crept tasks.

## Observability

### OpenTelemetry Tracing

Execution is automatically traced with full hierarchy via OTel:

```
Trace "planner: <goal>"
  ├── Span "react.step" #1
  │   ├── Span "step-1-llm"          ← prompt/completion tokens
  │   ├── Span "tool:plan_create"    ← input args + output result
  │   └── Span "tool:task_insert"
  ├── Span "reviewer.check"          ← drift assessment + token usage
  ├── Span "compress.summarize"      ← compression summary + token usage
  ├── Span "react.step" #2
  │   ├── Span "step-2-llm"
  │   └── Span "tool:worker_spawn"
  └── Span "memory.extract_before_compress"
```

Enable via `kugelblitz.yaml`:

```yaml
otel_enabled: true
otel_endpoint: "https://api.langfuse.com/api/public/otel/v1/traces"
otel_auth_header: "<base64(pk:sk)>"
otel_service_name: "kugelblitz"
```

Or in code:

```go
shutdown, _ := observability.InitTracer(ctx, cfg.Observability)
defer shutdown()
```

### Unified Usage Callback

A single callback reports **every LLM call's** token consumption, tagged by source:

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

Identities emitted: `planner.step-1`, `planner.step-2`, `compressor`, `reviewer`,
`worker.<task-id>`.

## Built-in Tools

### Intent & Routing

| Tool | Description |
|------|-------------|
| `set_work_mode` | Structured classification: analyze request → select mode ("plan" or "simple") |

### Plan & Task

| Tool | Description |
|------|-------------|
| `plan_create` | Create a new empty plan |
| `plan_query` | Query a plan by ID or list all |
| `confirm_plan` | Confirm or reject a plan after user review |
| `plan_rollback` | Rollback a plan to a previous checkpoint |
| `task_insert` | Insert a subtask into a plan |
| `task_delete` | Delete a task from a plan |
| `task_query` | Query a task by ID or list tasks in a plan |
| `task_status_update` | Update task status (pending → doing → done / failed) |

### Memory

| Tool | Description |
|------|-------------|
| `memory_store` | Manually store a memory item |
| `memory_search` | Semantic/keyword search (ChromaDB, fallback MEMORY.md). Supports `needGraph` |
| `memory_get_section` | List all items in a section |
| `memory_remove` | Delete an item |
| `memory_list_sections` | List all sections with counts |
| `memory_stats` | Stats: total items, sections, index status |
| `memory_resolve_conflict` | Confirm a pending conflict (keep_new / keep_old) |
| `memory_extract` | Agent-triggered: extract + persist from current session |

### File & Shell

| Tool | Description |
|------|-------------|
| `file_read` | Read a file from disk |
| `file_write` | Write/create a file on disk |
| `file_delete` | Delete a file |
| `file_copy` | Copy or move a file |
| `dir_create` | Create a directory |
| `dir_copy` | Copy or move a directory |
| `shell_exec` | Execute a shell command |

### Web

| Tool | Description |
|------|-------------|
| `web_search` | Search the web (DuckDuckGo by default) |
| `web_fetch` | Fetch a URL and convert HTML to Markdown |

### Interaction

| Tool | Description |
|------|-------------|
| `skill_use` | Activate a skill by name |
| `ask_human` | Pause and ask the human user for input |

## Directory Structure

```
kugelblitz/
├── core/              # Interfaces: ILMProvider, Observer, Span, Message, Tool, IAgent
├── config/            # Configuration structs (Model, Runtime, Compress, Drift)
├── constants/         # Enums: PlanState, RoleType, MultiModalType
├── runtime/           # Agent execution runtime
│   ├── agent_loop.go  #   AgentLoop — main entry point
│   └── engine/
│       ├── kernel.go  #   Kernel — public facade
│       ├── fsm/       #   State machine (State + Action + Machine)
│       ├── dag/       #   DAG task executor (topological batch execution)
│       └── infra/     #   Infrastructure (ReactAgent, Reviewer, WorkerAgent)
├── memory/
│   ├── session_memory.go  # SessionMemory — conversation history + auto-compress
│   ├── compressor.go      # LLM-based context summarization
│   ├── working/           # Working Memory (Plan + Task + Checkpoint)
│   └── longterm/          # Long-Term Memory (MEMORY.md + ChromaDB + Graph + Dream)
├── prompts/           # System prompt templates
├── observability/     # OpenTelemetry tracing + StepTracer instrumentation
├── tools/
│   ├── mcp/           # MCP server integration (client, manager, tool registry)
│   └── internals/     # Built-in tools (plan_*, task_*, memory_*, web, file, shell)
├── skills/            # Skill loader + registry
├── provider/
│   └── chat_completions/  # OpenAI-compatible Format (Block + Stream)
├── persist/           # Format-level stores: MarkdownPersist, JSONLPersist, VectorPersist
├── utils/             # UUID generation, session IDs
└── cmd/
    ├── common/        #   Shared YAML config helpers
    ├── kugelblitz-ui/ #   Web UI server (HTTP + SSE streaming)
    └── acp_server/    #   ACP editor agent
```

### Building Binaries

```bash
# Build all cmd binaries → bin/
make build

# Build a specific binary
make build build-cmds=kugelblitz-ui

# Build and install to $GOPATH/bin
make install
make install build-cmds=kugelblitz-ui PREFIX=/usr/local

# Start the Web UI (default :8088)
./bin/kugelblitz-ui
./bin/kugelblitz-ui -addr :9090

# Start the ACP server
./bin/acp_server -v
```

### Workspace Layout (`~/.kugelblitz/`)

```
~/.kugelblitz/
├── MEMORY.md                          # Long-term memory (authoritative, human-editable)
├── DREAMS.md                           # Dream diary (auto-generated reflections)
├── AGENTS.md                          # Agent capabilities (read-only)
├── IDENTITY.md                        # Agent identity (read-only)
├── SOUL.md                            # Agent personality (read-only)
├── USER.md                            # User profile (read-only)
├── kugelblitz.yaml                    # Main config (includes mcp_servers)
├── skills/
│   └── {name}/SKILL.md                # Skill definitions (read-only)
│
└── memory/                            # Agent-managed data
    ├── sessions/{id}.jsonl            # Session memory (JSONL)
    ├── plans/{planID}/
    │   ├── plan.jsonl                 # Working memory — Plan
    │   └── checkpoints/{v}.jsonl      # Plan version snapshots
    └── longterm/
        ├── memory_graph.jsonl         # Entity-relationship graph (JSONL)
        └── MEMORY_GRAPH.md            # Entity-relationship graph (Mermaid, read-only)
```
