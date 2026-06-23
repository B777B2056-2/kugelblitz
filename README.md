# Kugelblitz

[中文](README_zh.md)

A lightweight, modular **Agent Harness** for Go — scaffolding, lifecycle management,
and observability infrastructure that surrounds and orchestrates LLM-powered agents.

## Features

- **ReAct Engine** — think → act → observe loop with streaming and tool-call support
- **Planner + Worker** — dual-agent pattern: one plans, many execute
- **Plan-Execute-Adapt-Finish** — structured workflow with checkpoints and rollback
- **Session Memory** — automatic context compression when the window overflows
- **Long-Term Memory** — semantic deduplication via LLM judge
- **Goal-Drift Review** — periodic alignment check, automatic replan on drift
- **Skills** — pluggable domain-knowledge modules
- **Langfuse Observability** — full trace / span / generation hierarchy out of the box
- **Unified Usage Callback** — single callback for all LLM token consumption,
  tagged by source identity (planner, compressor, reviewer, worker)

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

    "kugelblitz/core"
    "kugelblitz/provider"
    "kugelblitz/runtime"
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

### Memory Harness

- **Session Memory** — full conversation history. When the context window overflows
  (`ErrContextLengthExceeded`), the harness **automatically compresses** old messages
  into an LLM-generated summary, keeping the most recent N messages intact.

- **Long-Term Memory** — key facts and lessons extracted from completed conversations
  and stored with **semantic deduplication**. An LLM judge resolves conflicts when
  new information contradicts existing memory.

### Skills

Skills are pluggable YAML/SKILL.md modules that inject domain knowledge into the
Planner's system prompt. Activate a skill via `skill_use` and the harness
automatically loads its instructions and tool definitions.

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
├── tools/
│   └── internals/     # plan_*, task_*, memory_*, worker_spawn, skill_use
├── skills/            # Skill loader + registry
├── provider/
│   └── chat_completions/  # OpenAI-compatible Format (Block + Stream)
├── persist/           # Plan checkpoint JSON, session JSONL
├── utils/             # UUID generation, session IDs
└── examples/
    ├── plan_mode/     # Full Planner demo
    ├── react/         # Standalone ReAct agent
    └── drift_demo/    # Drift detection demo
```
