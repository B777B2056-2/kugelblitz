# Human-in-the-Loop 示例

演示 agent 如何在执行过程中暂停并征询人类意见，待人类回复后继续执行。

## 运行

```bash
go run . -apikey sk-xxx
go run . -apikey sk-xxx -provider openai -model gpt-4.1-mini
```

## 工作流

```
用户目标: 查东京时间 → 提议安排明天站会 → 发日历邀请前问我确认

  LLM → get_current_time("Asia/Tokyo")     ← 查询时间
  LLM → 决定需要安排会议
  LLM → ask_human("要安排明天的站会吗?")    ← 触发暂停
       ┌──────────────────────────────────┐
       │ 🤖 Agent needs your input        │
       │ Question: 安排明早 10 点的站会?  │
       │ Your response: yes, go ahead     │  ← 你在控制台输入
       └──────────────────────────────────┘
  LLM → schedule_meeting(...)              ← 获批准后执行
  LLM → "会议已安排，明天 10:00 见。"       ← 回复用户
```

## 关键代码

```go
// 1. 启用人在回路 — 注册 ask_human 工具
agent.EnableHumanInTheLoop()

// 2. 注册回调 — 当 agent 等待人类输入时触发
waitSig := make(chan struct{}, 1)
agent.RegisterEventHooks(core.AgentEventHooks{
    OnWaitForHumanAction: func(reason, prompt string) {
        fmt.Printf("🤖 Agent needs your input:\n%s\n", prompt)
        waitSig <- struct{}{} // 通知主循环去读控制台输入
    },
})

// 3. 后台运行 agent
ctx, cancel := context.WithCancel(context.Background())
go func() {
    messages, _ = agent.Execute(ctx, systemMsg, userMessages)
    cancel()
}()

// 4. 主循环：等待信号 → 读控制台输入 → 送给 agent
for {
    select {
    case <-waitSig:
        scanner.Scan()
        agent.ResumeWithHumanResponse(ctx, scanner.Text())
    case <-ctx.Done():
        return // agent 执行完毕
    }
}
```

## 核心接口

| 方法 | 说明 |
|------|------|
| `agent.EnableHumanInTheLoop()` | 启用 HITL，注册 `ask_human` 本地工具 |
| `OnWaitForHumanAction(reason, prompt)` | 回调：agent 进入等待状态时触发 |
| `agent.HumanLoopWaiting() bool` | 查询 agent 是否正在等待人类输入 |
| `agent.ResumeWithHumanResponse(ctx, reply)` | 注入人类回复，解除暂停 |
