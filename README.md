# Kugelblitz

[中文](README_zh.md)

A lightweight, modular **Harness Agent** for Go — scaffolding, lifecycle management,
and observability infrastructure that surrounds and orchestrates LLM-powered agents.

## Features

- **ReAct Engine** — think → act → observe loop with streaming and tool-call support
- **Planner + Worker** — dual-agent pattern: one plans, many execute
- **Plan-Execute-Adapt-Finish** — structured workflow with checkpoints and rollback
- **Three-Layer Memory** — Session (conversation) + Working (plans/tasks) + Long-Term (MEMORY.md + ChromaDB index)
- **Goal-Drift Review** — periodic alignment check, automatic replan on drift
- **Skills** — pluggable domain-knowledge modules
- **Built-in Web Tools** — `web_search` (DuckDuckGo, zero-config) + `web_fetch` (HTML → Markdown, optional JS rendering)
- **Human-in-the-Loop** — agent can pause to consult a human, resume via
  `OnWaitForHumanAction` callback + `ResumeWithHumanResponse`
- **Langfuse Observability** — full trace / span / generation hierarchy out of the box
- **Unified Usage Callback** — single callback for all LLM token consumption,
  tagged by source identity (planner, compressor, reviewer, worker)
- **ACP (Agent Client Protocol)** — JSON-RPC 2.0 over stdio adapter, opt-in.
  Use any ACP-compatible editor (Zed, JetBrains, VS Code, Neovim) as the
  frontend for a Kugelblitz-powered agent

## Architecture

```
                         ┌──────────────────────────────┐
                         │       Kugelblitz Harness      │
 User Goal ─────────────►│                               │
                         │  ┌─────────────────────────┐  │
                         │  │ Planner                 │  │
                         │  │ Plan → Execute →        │  │
                         │  │ Adapt → Finish          │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ Tool Harness            │  │
                         │  │ plan_*, task_*,         │  │
                         │  │ memory_*, skill_use,    │  │
                         │  │ worker_spawn            │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ Memory Harness          │  │
                         │  │ session + LTM           │  │
                         │  │ + auto-compress         │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ Review Harness          │  │
                         │  │ drift detection         │  │
                         │  │ + auto-rollback         │  │
                         │  └───────────┬─────────────┘  │
                         │              │                │
                         │  ┌───────────▼─────────────┐  │
                         │  │ Observability           │  │
                         │  │ Langfuse traces +       │  │
                         │  │ unified usage callback  │  │
                         │  └─────────────────────────┘  │
                         └──────────────────────────────┘
```

## Quick Start

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
        "Create a README.md describing the project features, then add a docs/architecture.md")
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

Run with optional Langfuse tracing:

```bash
go run . -apikey sk-xxx \
  -langfuse-host http://localhost:3000 \
  -langfuse-pk pk-xxx \
  -langfuse-sk sk-xxx
```

## Core Concepts

### Planner

The Planner follows a **Plan → Execute → Adapt → Finish** workflow. It decomposes the
user's goal into subtasks, spawns Worker agents to handle independent tasks in parallel,
and monitors progress. Failed tasks trigger adaptation (replan or retry). When all tasks
are done the Planner summarizes and finishes.

### Worker

Workers execute discrete subtasks with a restricted tool set (file read/write, directory
operations). The Planner spawns them via `worker_spawn`, and they run concurrently when
tasks are independent.

### Memory System

Kugelblitz provides a three-layer memory architecture: **Session Memory** (short-term,
conversation history), **Working Memory** (plans and tasks in progress), and
**Long-Term Memory** (persistent knowledge across sessions).

```
┌─────────────────────────────────────────────┐
│              Session Memory                 │  ← in-memory, per-conversation
│  Messages + Summary (auto-compressed)       │
├─────────────────────────────────────────────┤
│              Working Memory                 │  ← plans + tasks + checkpoints
│  Plan / Task / Checkpoint (JSONL)           │
├─────────────────────────────────────────────┤
│              Long-Term Memory               │  ← persistent, cross-session
│  MEMORY.md (authoritative) + ChromaDB (index)│
└─────────────────────────────────────────────┘
```

#### Session Memory

Full conversation history kept in memory. Persisted as JSONL to disk after every
`Planner.Execute()` and after compression. When the context window overflows
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

### Skills

Skills are pluggable YAML/SKILL.md modules that inject domain knowledge into the
Planner's system prompt. Activate a skill via `skill_use` and the harness
automatically loads its instructions and tool definitions.

### Web Tools

Built-in tools that give agents internet access — register with a single call:

```go
internals.RegisterWebTools(nil) // DuckDuckGo (free, no API key)
```

**`web_search`** — search the web via DuckDuckGo with zero configuration.
Returns structured results (title, URL, snippet). Supports custom backends
(e.g. Brave Search) via the `SearchBackend` interface.

**`web_fetch`** — fetch a URL and convert it to clean Markdown using
`html-to-markdown`. Scripts, styles, and navigation are automatically stripped.
Set `render_js: true` to render SPAs with a headless browser before extraction.

```
web_search (query, limit?)  → {query, results[{title, url, snippet}]}
web_fetch  (url, render_js?) → {url, title, markdown}
```

## ACP — Agent Client Protocol

Kugelblitz 可作为 ACP-compatible Agent 运行，接入任何支持
[Agent Client Protocol](https://agentclientprotocol.com) 的编辑器（Zed、JetBrains、
VS Code、Neovim 等）。

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
- **Planner / Worker default** — enabled by default in `NewPlanner` and `NewWorkerAgent`

### Example

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

Full example at [examples/human_in_the_loop/](examples/human_in_the_loop/).

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

## Harness — Self‑Healing & Drift Prevention

The harness provides two built-in safety mechanisms that operate **without human
intervention** to keep execution on track.

### Planner Self‑Healing

When tool calls fail repeatedly (`FailuresBeforeReview` consecutive failures), the
Planner **automatically rolls back** to the last known-good checkpoint:

```
Step N   ──❌ fail
Step N+1 ──❌ fail
Step N+2 ──❌ fail
    → FailuresBeforeReview (default 3) reached
    → Load checkpoint from version N-1
    → Restore Plan to that checkpoint
    → Inject system message: "plan rolled back to vN-1"
    → Continue execution from the safe point
```

This prevents the model from compounding errors and gives it a clean path to
recovery — **no human intervention required**.

Configuration:

```go
planner.SetReviewConfig(runtime.ReviewConfig{
    FailuresBeforeReview: 3,  // trigger after 3 consecutive failures
})
```

### Reviewer Drift Detection

Two triggers independently activate the Reviewer:

1. **ReAct step interval** — every N steps (`ReActStepInterval`), regardless of
   success or failure.
2. **Consecutive failures** — after N consecutive tool failures
   (`FailuresBeforeReview`).

The Reviewer sends the **original goal**, **current plan state**, and **recent
activity** to an LLM that uses a `reviewer_report` tool to return structured
output:

```go
type ReviewResult struct {
    Drift      bool        // true if execution has deviated
    Reason     string      // explanation
    Suggestion string      // suggested corrective action
}
```

If `Drift == true`, the harness triggers `replan()`, rolling back to the previous
checkpoint version — just like the self-healing path.

This keeps the Planner focused on the **original goal** and prevents token waste
on irrelevant or scope-crept tasks.

Configuration:

```go
planner.SetReviewConfig(runtime.ReviewConfig{
    ReActStepInterval:    8,  // review every 8 ReAct steps
    FailuresBeforeReview: 3,  // review after 3 consecutive failures
})
```

### Tool Result Compression

When a tool returns a large string value (e.g. `file_read` of a 10000-line file,
or `web_fetch` of a long page), the harness **automatically compresses** that field
via the LLM before it enters the conversation context — preventing token waste and
context overflow.

- **Per-field** — only individual string values that exceed the threshold are
  compressed; other fields (paths, numbers, booleans, short strings) stay intact.
- **Error-safe** — results containing an `error` key are never compressed.
- **Configurable** — threshold is set via `WithMaxToolResultChars` (default 4000 UTF-8 chars, 0 = disable).

```go
planner := runtime.NewPlanner(p, true,
    runtime.WithMaxToolResultChars(2000), // compress strings > 2000 chars
)
```

Compressed fields are replaced in-place within `Outputs`, so the Planner sees
the summarized version while the tool span in Langfuse still records the original.

### Harness Flow Summary

```
        ┌──────────┐
        │  Execute  │
        └─────┬─────┘
              │
    ┌─────────▼─────────┐
    │  ReAct Step Loop  │◄──────────────────────────┐
    └─────────┬─────────┘                           │
              │                                     │
    ┌─────────▼─────────┐     ┌──────────────┐      │
    │  Tool Results     │────►│  Failures?   │      │
    └───────────────────┘     └──────┬───────┘      │
                                     │              │
                          ┌──────────▼──────────┐   │
                          │  ≥ FailuresBefore   │   │
                          │  Review threshold?   │   │
                          └──────────┬───────────┘   │
                                     │              │
                          ┌──────────▼──────────┐   │
                          │  Reviewer checks    │   │
                          │  for goal drift     │   │
                          └──────────┬──────────┘   │
                                     │              │
                          ┌──────────▼──────────┐   │
                          │  Drift detected?    │───┤
                          │  → replan(rollback) │   │
                          └─────────────────────┘   │
                                                    │
                          ┌─────────────────────────┘
                          │
                ┌─────────▼─────────┐
                │  Step interval    │
                │  ≥ ReActStep      │──► Reviewer check
                │  Interval?        │
                └───────────────────┘
```

## Observability

### Langfuse Tracing

Planner execution is automatically traced with full hierarchy:

```
Trace "session-<uuid>"
  ├── Span "react.step" #1
  │   ├── Generation "step-1-llm"     ← prompt/completion tokens
  │   ├── Span "tool:plan_create"     ← input args + output result
  │   └── Span "tool:task_insert"
  ├── Span "context.compress"         ← compression summary + token usage
  ├── Span "react.step" #2
  │   ├── Generation "step-2-llm"
  │   └── Span "tool:worker_spawn"
  └── Span "reviewer.check"           ← drift assessment + token usage
```

Enable with a single option:

```go
lfObs := observability.NewLangfuseObserver(observability.LangfuseConfig{
    Host:      "http://localhost:3000",
    PublicKey: "pk-xxx",
    SecretKey: "sk-xxx",
})
planner := runtime.NewPlanner(p, true, runtime.WithObserver(lfObs))
```

### Unified Usage Callback

A single callback reports **every LLM call's** token consumption, tagged by source:

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

Identities emitted: `planner.step-1`, `planner.step-2`, `compressor`, `reviewer`,
`worker.<task-id>`.

## Directory Structure

```
kugelblitz/
├── core/              # Interfaces: Observer, Span, Message, Tool, Workspace
├── runtime/           # Planner, ReactAgent, WorkerAgent, Reviewer
├── memory/            # SessionMemory, Compressor, LongTermMemory
├── observability/     # LangfuseObserver, PlannerInstrument
├── acp/               # ACP adapter (JSON-RPC 2.0 stdio transport, session mgmt)
├── tools/
│   └── internals/     # plan_*, task_*, memory_*, worker_spawn, skill_use
├── skills/            # Skill loader + registry
├── provider/
│   └── chat_completions/  # OpenAI-compatible Format (Block + Stream)
├── persist/           # Plan checkpoint JSON, session JSONL
├── utils/             # UUID generation, session IDs
└── examples/
    ├── plan_mode/            # Full Planner demo
    ├── react/                # Standalone ReAct agent
    ├── acp_server/           # ACP server (editor-compatible agent)
    ├── drift_demo/           # Drift detection demo
    └── human_in_the_loop/    # Human-in-the-loop demo
```
