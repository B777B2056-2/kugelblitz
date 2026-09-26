# Bug Register — Phase 0 审计产物（完整版）

分支：`fix/functional-issues`（基于 `main` @ 79cbe49 切出）

## 0. 基线状态（2026-09-26 实测）

| 检查 | 结果 |
|---|---|
| `go build ./...`（主模块 + 3 cmd 子模块） | ✅ 全干净 |
| `go vet ./...` | ✅ 无告警 |
| `go test ./... -count=1`（21 包 + 3 子模块） | ✅ 全绿（**无 race 检测**） |
| `go test -race ./...` | ⚠️ 本机不可用（Windows 无 gcc/cgo） |
| `golangci-lint run` | ⚠️ 未安装 |

> 关键结论：现有测试套件**绿但不覆盖并发与静默失败路径**。竞态类缺陷（B1/B2/B5/B7/A3/U3/U4/U5/T9/P9）为代码审查确认，需在带 gcc 的环境（Linux CI）用 `-race` 复现。

## 统计

| 严重度 | 数量 | 说明 |
|---|---|---|
| P0 | 9 | 崩溃 / 竞态 / 数据损坏 / 死循环 |
| P1 | 22 | 静默失败 / 语义错误 / 功能失效 |
| P2 | 29 | 数据完整性 / 非确定 / 次要缺陷 |
| P3 | 1 | 轻微 |
| **合计** | **61** | 覆盖 runtime/core、persist、tools、acp、ui 五层 |

---

## 1. 运行时 / 核心（runtime、engine、memory、core、provider）

### P0 — 崩溃 / 竞态 / 数据损坏

| ID | 位置 | 问题 | 修复方向 |
|---|---|---|---|
| B1 | [dag.go:27](runtime/engine/dag/dag.go#L27) | `hitlAgents` 普通 map 无锁并发读写 | `sync.Map` 或加锁 |
| B2 | [dag.go:96-99,69-72](runtime/engine/dag/dag.go#L96-L99) | `RWMutex` 当暂停门 + `defer Resume()` 无条件 Unlock → panic | 计数 + `sync.Cond` |
| B3 | [agent_loop.go:98-101,329](runtime/agent_loop.go#L98-L101) | `initLTM` 吞错 → `extractMemories` nil panic | 返回 error / 可选降级 |
| B4 | [worker.go:37](runtime/engine/infra/worker.go#L37) | `maxSteps` 死字段，ReAct 无限循环 | 接入步数上限 |
| B5 | [dag.go:114,180-187](runtime/engine/dag/dag.go#L114) | `d.cancel` 共享字段；持 `planMu` 做 LLM 调用 | cancel 局部化；先释放锁 |
| B6 | [session_memory.go:103-107](memory/session_memory.go#L103-L107) | `Compress` 写锁覆盖期间丢消息 | 合并新增消息 |
| B7 | [session_memory.go:177-184](memory/session_memory.go#L177-L184) | `CreateSessionMemory` 并发 lost-update | `LoadOrStore` |
| B8 | [working.go:128-129](memory/working/working.go#L128-L129) | `Persist`/`SaveCheckpointJSON` 错误吞掉（checkpoint 丢失） | 上抛 error |
| B10 | 全项目 | ~30 处 `_ =` 错误抑制未分级 | 按 §6 规范逐条 |

### P1 — 语义错误 / 静默失败

| ID | 位置 | 问题 | 修复方向 |
|---|---|---|---|
| B11 | [prompts.go:66,74](prompts/prompts.go#L66) | `MustRender` 模板错误 panic | 增加 `Render`(error) 版本 |
| B12 | [machine.go:94-97](runtime/engine/fsm/machine.go#L94-L97) | 撞 `MaxCycles` 静默返回 nil | 返回哨兵错误 |
| B13 | [state.go:82-86,141,222](runtime/engine/fsm/state.go#L82-L86) | Intent/Init/Updating 失败静默降级 Direct | 返回错误/显式 DefaultMode |
| B14 | [reviewer.go:84-86](runtime/engine/infra/reviewer.go#L84-L86) | 解析失败 fail-open + 未检查断言 | 显式标记 + 可配 fail-safe |
| B15 | [action.go:139](runtime/engine/fsm/action.go#L139) | 漂移"每 N 步"失效（StepCount 语义错） | 用真实 ReAct 步数 |
| B16 | [react.go:61-68](runtime/engine/infra/react.go#L61-L68) | `WithTools` append 累积 | 改替换语义 |
| B17 | [worker.go:127-168](runtime/engine/infra/worker.go#L127-L168) | Worker 输出重复拼接 | 取单一来源 |
| B18 | [converter.go:320](provider/chat_completions/converter.go#L320) | 强制 `Strict:true` 与 schema 不合规 | 可配置/校验 |
| B19 | [tool.go:77-85](core/tool.go#L77-L85) | `ListDefinitions` map 无序 | 按 name 排序 |
| B20 | [working.go:107-116](memory/working/working.go#L107-L116) | checkpoint 共享 `Task.Usage` 指针 | 深拷贝 |
| B21 | [agent_loop.go:80](runtime/agent_loop.go#L80) | MCP 连接 Background ctx、退出不 Shutdown | 纳入生命周期 |
| B22 | 多处 | `context.Background()` 丢取消/trace | 透传 ctx |

### P2 — 死代码 / 次要

| ID | 位置 | 问题 |
|---|---|---|
| B23 | [state.go:14](runtime/engine/fsm/state.go#L14) | `State.AvailableTools()` 死接口 |
| B24 | [converter.go:277](provider/chat_completions/converter.go#L277) | `convertToolMessage` 无调用方 |
| C1 | [media.go:138](core/media.go#L138) | `http.DetectContentType` 识别不了白名单内 audio/webm、video/quicktime 等 → 合法输入被误拒 / meta 缺失 |
| C2 | [message.go:175,204](core/message.go#L175) | `json.Marshal` 错误吞掉；composite 子块解析失败静默跳过（留 nil） |
| C3 | [workspace.go:70-81](core/workspace.go#L70-L81) | `SessionPath`/`PlanPath` 无路径穿越防护（sessionID 可被 ACP 客户端控制） |
| C4 | [logger.go:22](core/logger.go#L22) | `SetLogger` 与日志调用无锁，运行时替换有竞态 |

---

## 2. persist 存储层

### P1

| ID | 位置 | 问题 |
|---|---|---|
| P1 | [jsonl_persist.go:52](persist/jsonl_persist.go#L52) | `Append` 忽略 `Load` 错误 → **静默清空已有 JSONL 数据** |
| P2 | [jsonl_persist.go:51-68](persist/jsonl_persist.go#L51-L68) | `Append` 读-改-写非原子 → 并发追加丢事件 |
| P3 | [chroma_store.go:41](persist/chroma_store.go#L41) | `NewChromaStoreOrNil` 吞初始化错误 → 向量功能静默失效 |
| P4 | [chroma_store.go:187-209](persist/chroma_store.go#L187-L209) | `Exists` 对不存在 key 恒返回 true（逻辑反转） |
| P5 | [chroma_store.go:182-198](persist/chroma_store.go#L182-L198) | `Store`/`Delete` doc ID 不匹配 → **Delete 永远删不掉** |
| P6 | [chroma_store.go:99-115](persist/chroma_store.go#L99-L115) | `Search` 忽略 `SearchMode` → BM25/hybrid 名存实亡 |

### P2

| ID | 位置 | 问题 |
|---|---|---|
| P7 | [chroma_store.go:56,84,105,164,215](persist/chroma_store.go#L56) | 多处 `json.Marshal` 吞错 → 发空请求体 |
| P8 | [chroma_store.go:129-131](persist/chroma_store.go#L129-L131) | 距离转分数公式产生负分/无效分数 |
| P9 | [file_persist.go:65-85](persist/file_persist.go#L65-L85) | `List` 未加锁（与 Store/Delete 竞态） |
| P10 | [checkpoint.go:33-47](persist/checkpoint.go#L33-L47) | `ListCheckpoints` 对完整路径 `Atoi` → 恒返回空 |
| P11 | [session.go:56-58](persist/session.go#L56-L58) | `ListSessions` 返回带前缀路径而非裸 ID |
| P12 | [plan.go:24-25](persist/plan.go#L24-L25) | `fmt.Errorf("%w", nil)` 产生畸形错误 |
| P13 | [session.go:43-50](persist/session.go#L43-L50) | `LoadSessionJSONL` 静默丢弃反序列化失败的消息 |
| P14 | [file_persist.go:36](persist/file_persist.go#L36) | `Store` 非原子写 → 崩溃留下截断文件 |
| P15 | [markdown_persist.go:122-140](persist/markdown_persist.go#L122-L140) | `formatMarkdown` 大小写分组丢条目 |

---

## 3. tools/internals 工具层

| ID | 严重度 | 位置 | 问题 |
|---|---|---|---|
| T1 | P1 | [shell_exec.go:92-104](tools/internals/shell_exec.go#L92-L104) | 超时被静默吞掉，超时命令返回"成功"（`ctx.Err()` 分支死代码） |
| T2 | P2 | [web_fetch.go:98-99](tools/internals/web_fetch.go#L98-L99) | 按字节截断破坏多字节 UTF-8 |
| T3 | P2 | [shell_exec.go:116-121](tools/internals/shell_exec.go#L116-L121) | 输出截断同样破坏 UTF-8 |
| T4 | P2 | [file_copy.go:64-69](tools/internals/file_copy.go#L64-L69) | move 回退吞 `os.Remove` 错误 → 报"已移动"但源文件残留 |
| T5 | P2 | [dir.go:93-100](tools/internals/dir.go#L93-L100) | move 回退吞 `os.RemoveAll` 错误 |
| T6 | P2 | [web_search.go:249](tools/internals/web_search.go#L249) | fallback 负索引切片 panic 风险 |
| T7 | P2 | [plan.go:475-481](tools/internals/plan.go#L475-L481) | `PlanRollback` version 类型校验缺失 → 静默回滚到错误检查点 |
| T8 | P3 | [shell_exec.go:59](tools/internals/shell_exec.go#L59) / [ask_human.go:55](tools/internals/ask_human.go#L55) | `Arg` 类型错误被 `_` 丢弃 |
| T9 | P2 | [plan.go:166-497](tools/internals/plan.go#L166) | plan 工具对共享 `*Plan` 字段无锁读写（数据竞态） |

---

## 4. cmd/acp_server（ACP 协议）

| ID | 严重度 | 位置 | 问题 |
|---|---|---|---|
| A1 | P1 | [handler.go:59-107](cmd/acp_server/handler.go#L59-L107) | `session/cancel`、`session/delete` 成功时返回 nil → **永不回包，客户端挂起** |
| A2 | P1 | [handler.go:157-261](cmd/acp_server/handler.go#L157-L261) | `session/prompt` 单线程同步跑 agent → **执行期间 cancel 读不到** |
| A3 | P2 | [session.go:109-134](cmd/acp_server/session.go#L109-L134) | `cancelFn` 读/写无锁竞态 |
| A4 | P2 | [handler.go:114,196-304](cmd/acp_server/handler.go#L114) | transport 写/通知/会话追加失败被 `_` 吞掉 |

---

## 5. cmd/kugelblitz-ui（Web UI）

| ID | 严重度 | 位置 | 问题 |
|---|---|---|---|
| U1 | P1 | [chat.go:94](cmd/kugelblitz-ui/chat.go#L94) | **聊天记录从未持久化**（`addTurnMessage` 仅媒体预处理调用一次） |
| U2 | P2 | [session.go:372-391](cmd/kugelblitz-ui/session.go#L372-L391) | 非首个 report 的 input 被计入 output（token 双计） |
| U3 | P2 | [chat.go:155,211-244](cmd/kugelblitz-ui/chat.go#L155) | `capturedToolCalls` 无锁 append/read/reset 竞态 |
| U4 | P2 | [chat.go:218](cmd/kugelblitz-ui/chat.go#L218) | `currentPlan` 无锁读（与 derivePlanUpdate 竞态） |
| U5 | P2 | [chat.go:43-50](cmd/kugelblitz-ui/chat.go#L43-L50) | `hitlCh` 无锁重建 + 其他 per-turn 状态无锁重置 |
| U6 | P2 | [session.go:138,211,269,311](cmd/kugelblitz-ui/session.go#L211) | 会话持久化/删除失败被 `_` 吞掉 |

---

## 6. `_ =` 错误抑制规范（B10 处理准则）

审计结论：全项目 ~30 处 `_ =`，按三类处理：

1. **持久化/数据完整性（必须上抛 error）**：`working.Persist`、`SaveCheckpointJSON`、`ltm.write`（fact.go x3）、`sessionMem.Persist`、`plan.Persist`、`saveStoredSession`（U6）。
2. **IO 写失败（必须记日志）**：ACP transport 写/通知（A4）、UI `json.Encode`、`os.Remove`/`os.MkdirAll`。
3. **可接受（补注释统一写法）**：`defer resp.Body.Close()`、`tp.Shutdown`、`g.Wait()`（errgroup 仅做同步）、`stepTracer.SetTrace` 的 ctx。

> 更正：`jsonl_persist.go:99` 的 `_ = bw.Flush()` 经核实是 `bufio.Writer` 包 `bytes.Buffer`，Flush 到内存不失败，**非缺陷**。

## 7. `nolint:staticcheck` 审计

| 位置 | 判断 |
|---|---|
| [converter.go:1](provider/chat_completions/converter.go#L1) | 待审（openai-go param 用法） |
| [format.go:1](provider/chat_completions/format.go#L1) | 待审 |
| [react.go:377](runtime/engine/infra/react.go#L377) | 与 B2 同源，重构时消除 |

## 8. 环境限制与对策

- **race 检测**：本机无 gcc。对策：(a) 安装 mingw-w64 或 (b) Linux CI 跑 `-race`。竞态类缺陷（B1/B2/B5/B7/A3/U3/U4/U5/T9/P9）先写测试，上 CI 复现。
- **lint**：`golangci-lint` 未装，先用 `go vet`（已跑，干净）。

## 9. 审计完成状态

- [x] runtime/engine（fsm、dag、infra、kernel、agent_loop）
- [x] core（message、media、tool、hooks、logger、workspace、context、input、errors）
- [x] provider/chat_completions（converter、format）
- [x] memory（session、working、longterm）
- [x] persist 全 10 文件
- [x] tools/internals 全 14 文件
- [x] cmd/acp_server、cmd/kugelblitz-ui、cmd/common
- [ ] 未逐行审计：`observability/steptracer.go` 内部逻辑、`prompts/` 模板内容正确性（模板文本本身）

**下一步（Phase 1）**：按 P0 → P1 → P2 顺序，每条写失败测试（红）→ 修复（绿）。建议从 P0 九条（B1-B8、B10）开始。
