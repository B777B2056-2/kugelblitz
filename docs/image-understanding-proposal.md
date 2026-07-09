# Kugelblitz 框架层多模态理解能力 — 设计文档

> 版本: v3.0 | 日期: 2026-07-09 | 分支: feat-multimodal

---

## 一、现状与目标

### 1.1 已有基础

| 层面 | 已有 | 缺失 |
|------|------|------|
| 类型枚举 | `MultiModalType`: image, video, audio, pdf, word, excel, text | — |
| Message | `MultiModalDetail{ID, Type, Path, Base64}` + `MultiModalContent`，JSON 序列化已支持 | 缺 `MimeType`、`Meta` 字段；持久化会写入 Base64 |
| Provider 接收 | `ParseResponse` 可将 `ChatCompletionAudio` 转为 `MultiModalContent` | — |
| MCP 工具 | `ImageContent` 已提取为 `{"mime_type":"...", "data":"..."}` | — |
| **Provider 发送** | — | `extractText()` 只接受 `TextContent`，多模态报错 |
| AgentLoop | `Run(ctx, goal string)` | 只能传文本 |
| SessionMemory | `[]Message` 可容纳 `MultiModalContent` | Compressor 只处理文本，遇多模态无输出 |
| LongTermMemory | MEMORY.md 纯文本 KV | 无法引用媒体 |
| Tools | `file_read` 读文件为文本 | 无 MIME 检测、无图像读取 |

### 1.2 目标

框架层支持 **image / video / audio** 三种 LLM API 原生多模态类型。PDF/Word/Excel 等文档格式由工具层负责转换后传入。

### 1.3 核心瓶颈

`provider/chat_completions/converter.go:77` 的 `extractText()` — 只接受 `TextContent`，**唯一阻断点**。

### 1.4 核心设计原则

**通用管道 + 类型策略**：所有媒体类型共享同一条处理管道，仅在三个接入点按 `MultiModalType` 分流：

```
输入 → ①校验 → ②描述 → ③Provider传输 → LLM
           ↑        ↑          ↑
       MIME白名单  描述Prompt  ContentPart类型
       大小限制    (元数据免费  (image_url
       元数据提取   +LLM可选)   vs input_audio)
```

**Compressor / Extractor / Dreamer 零感知**：媒体在进入 SessionMemory 之前产出文本描述，以 `CompositeContent{TextContent, MultiModalContent}` 存储。下游只处理 `TextContent`。

**框架层范围**：image / video / audio。文档格式（pdf/word/excel）由工具层负责转换。

---

## 二、Core 类型体系

### 2.1 MultiModalDetail 扩展

```go
// core/message.go

type MultiModalDetail struct {
    ID       string                   `json:"id"`
    Type     constants.MultiModalType `json:"type"`
    Path     string                   `json:"path,omitempty"`
    Base64   string                   `json:"base64,omitempty"`
    MimeType string                   `json:"mime_type,omitempty"` // 新增
    Meta     map[string]any           `json:"meta,omitempty"`      // 新增: 通用元数据桶
}

// MarshalJSON 持久化时剥离 Base64，避免 JSONL 膨胀
func (m MultiModalDetail) MarshalJSON() ([]byte, error) {
    type safe MultiModalDetail
    s := safe(m)
    s.Base64 = ""
    return json.Marshal(s)
}
```

Meta 示例：
```go
// image
Meta: {"width": 1920, "height": 1080}
// audio
Meta: {"duration_sec": 120.5, "sample_rate": 44100, "channels": 2}
// video
Meta: {"width": 1920, "height": 1080, "duration_sec": 45.0, "fps": 30}
```

### 2.2 AgentInput — 统一入口

```go
// core/agent_input.go

type AgentInput struct {
    Text  string             // 用户文本
    Media []MultiModalDetail // 可选媒体附件
}

func (ai AgentInput) IsTextOnly() bool { return len(ai.Media) == 0 }

func (ai AgentInput) BuildUserMessage() Message {
    if ai.IsTextOnly() {
        return NewUserMessage(TextContent{Text: ai.Text})
    }
    parts := make([]Content, 0, len(ai.Media)+1)
    parts = append(parts, TextContent{Text: ai.Text})
    for _, m := range ai.Media {
        parts = append(parts, MultiModalContent{Detail: m})
    }
    return NewUserMessage(CompositeContent{Parts: parts})
}
```

**AgentInput 贯穿全链路**：

```
AgentLoop.Run(ctx, input AgentInput)
  └─ execute()
       └─ Kernel.Run(ctx, input)           ← 收 AgentInput
            └─ fsm.Machine.Run(ctx, input) ← 收 AgentInput
                 └─ ReactAction:
                      msg := input.BuildUserMessage()  ← TextOnly 则 TextContent
                      sessionMem.AppendMessage(msg)     ← 有媒体则 CompositeContent
```

纯文本路径（`input.IsTextOnly() == true`）：`BuildUserMessage()` 返回 `TextContent`，行为与旧版完全一致。

| 调用方 | 旧写法 | 新写法 |
|--------|--------|--------|
| Web UI | `loop.Run(ctx, req.Goal)` | `loop.Run(ctx, core.AgentInput{Text: req.Goal, Media: media})` |
| 内部测试 | `loop.Run(ctx, "test")` | `loop.Run(ctx, core.AgentInput{Text: "test"})` |

### 2.3 MediaTypeValidator 接口

```go
// core/media.go

type MediaTypeValidator interface {
    Type()           constants.MultiModalType
    MIMEWhitelist()  []string
    MaxSize()        int64
    ExtractMeta([]byte) map[string]any
}

type MediaValidatorRegistry struct {
    validators map[constants.MultiModalType]MediaTypeValidator
}

func NewDefaultRegistry() *MediaValidatorRegistry {
    return &MediaValidatorRegistry{
        validators: map[constants.MultiModalType]MediaTypeValidator{
            constants.MultiModalTypeImage: &imageValidator{},
            constants.MultiModalTypeAudio: &audioValidator{},
            constants.MultiModalTypeVideo: &videoValidator{},
        },
    }
}
// 未注册类型 → 拒绝，无 fallback
```

内置校验器：

| 类型 | MIME 白名单 | 大小上限 | 元数据提取 |
|------|-----------|---------|-----------|
| image | png, jpeg, gif, webp | 20MB | `image.DecodeConfig` → width, height |
| audio | mpeg, wav, mp4, webm | 50MB | nil（待实现） |
| video | mp4, webm, quicktime | 100MB | nil（待实现） |

---

## 三、媒体预处理管道

### 3.1 MediaPreprocessor — 通用预处理

```go
// core/media.go

type MediaPreprocessor struct {
    validators *MediaValidatorRegistry
}

// Normalize 将任意媒体输入标准化：
// 1. 判断来源: Base64≠"" → 解码；Path 含 "://" → 下载(未实现)；否则 → 读文件
// 2. http.DetectContentType → MIME
// 3. 查 validator，校验 MIME ∈ 白名单
// 4. 校验大小 ≤ MaxSize
// 5. 若 Base64 为空 → base64.StdEncoding.Encode
// 6. ExtractMeta → Meta
func (p *MediaPreprocessor) Normalize(ctx context.Context, detail MultiModalDetail) (*MultiModalDetail, error)
```

输入输出统一：不管来自文件路径、URL 还是 base64，`Normalize` 结束后的 `detail` 保证 `Base64` 有效、`MimeType` 已检测、`Meta` 已提取。

### 3.2 MediaConverter — base64 ↔ 文件系统

```go
// memory/media_converter.go

type MediaConverter struct { baseDir string }

// Base64ToFile: 解码 base64 → 写入 media/{sessionID}/{contentID}.{ext} → 更新 Path
// FileToBase64: 从文件读取 → base64 编码 → 恢复传输格式
// Remove / PruneSession
```

持久化规则：
```
存 JSONL 前:  Base64ToFile() → 文件就绪 → MarshalJSON() 剥离 Base64 → JSONL 只存 Path
恢复 JSONL 后: UnmarshalJSON() → 得 Path → FileToBase64() → 恢复 Base64 → Provider 可用
```

### 3.3 MediaDescriber — 两层文本描述

```go
// memory/media_describe.go

type MediaDescriber struct {
    provider core.ILMProvider  // nil = 只用元数据层
    prompts  map[constants.MultiModalType]string
}

// Describe: 始终返回文本
//   第一层(免费): 元数据摘要 → "[image: image/png 1920×1080]"
//   第二层(可选): 若 provider≠nil 且已注册 prompt → LLM 增强描述
func (d *MediaDescriber) Describe(ctx context.Context, detail core.MultiModalDetail) string
```

默认 Prompt：
```go
constants.MultiModalTypeImage: "请用中文简要描述这张图片的内容和关键信息，不超过200字。"
constants.MultiModalTypeAudio: "请用中文总结这段音频的内容要点，包括说话人意图和关键信息，不超过200字。"
constants.MultiModalTypeVideo: "请用中文概述这个视频的画面内容和关键场景，不超过200字。"
```

### 3.4 BuildMediaMessage — 组装消息

```go
func BuildMediaMessage(ctx context.Context, d *MediaDescriber, detail core.MultiModalDetail) Message {
    desc := d.Describe(ctx, detail)
    return NewUserMessage(CompositeContent{
        Parts: []Content{
            TextContent{Text: desc},            // Compressor/Extractor 消费
            MultiModalContent{Detail: detail},  // Provider 消费
        },
    })
}
```

---

## 四、Provider 传输层

```go
// provider/chat_completions/converter.go

// 原代码:
//   case constants.RoleUser:
//       text, _ := extractText(message.Content)
//       return openai.UserMessage(text), nil

// 改为:
//   case constants.RoleUser:
//       return c.convertUserMessage(message)
```

```go
func (c *Converter) convertUserMessage(msg Message) (ParamUnion, error) {
    switch ct := msg.Content.(type) {
    case TextContent:        return openai.UserMessage(ct.Text), nil
    case MultiModalContent:  return c.singleMedia(ct.Detail)
    case CompositeContent:   return c.compositeUser(ct.Parts)
    }
}

func (c *Converter) mediaContentPart(detail MultiModalDetail) (ContentPartUnion, error) {
    switch detail.Type {
    case MultiModalTypeImage: return ImageContentPart(image_url{dataURL, detail:"auto"}), nil
    case MultiModalTypeAudio: return InputAudioContentPart(data: detail.Base64, format: audioFormat(detail.MimeType)), nil
    case MultiModalTypeVideo: return ImageContentPart(image_url{dataURL, detail:"low"}), nil   // 首帧
    default:                  return error("unsupported media type")
    }
}
```

---

## 五、记忆系统（零改动组件）

### 5.1 短期记忆

媒体消息以 `CompositeContent{TextContent(描述), MultiModalContent(媒体)}` 存入 SessionMemory。

| 组件 | 实际行为 |
|------|---------|
| **Compressor** | 遍历 Parts 时提取 `TextContent`，自然写入摘要 |
| **BuildSummarizePrompt** | 无需 `case MultiModalContent` |
| **JSONL 持久化** | `MarshalJSON` 剥离 Base64，只存 Path + Meta |

无论 `AutoDescribeMedia` 是否开启，Compressor 永远看到文本：
- `true`: "用户登录页面的 nginx 500 错误截图"
- `false`: "[image: image/png 1920×1080]"

### 5.2 长期记忆

Extractor 处理对话历史时，描述 `TextContent` 作为普通文本出现在 `## Conversation` 段中，与其它消息一视同仁。**Dreamer 零改动**。

### 5.3 记忆链路零改动清单

```
prompts/llm_prompts.go       — 不改
memory/compressor.go         — 不改
memory/longterm/extractor.go — 不改
memory/longterm/dream.go     — 不改
```
> Kernel/FSM 随 AgentInput 透传而改动签名，但不改变记忆处理逻辑。

---

## 六、配置

```go
// config/config.go

type MultimodalConfig struct {
    ImageModel        *ModelConfig `json:"image_model,omitempty"`       // nil = 回退到主模型
    AudioModel        *ModelConfig `json:"audio_model,omitempty"`       // nil = 回退到主模型
    AutoDescribeMedia bool         `json:"auto_describe_media"`         // 是否自动 LLM 描述
}
```

YAML：
```yaml
image_provider_name: openai
image_model: gpt-4o
auto_describe_media: true
```

---

## 七、AgentLoop API 重构

```
旧: AgentLoop.Run(ctx, goal string)
新: AgentLoop.Run(ctx, input core.AgentInput)
```

**AgentInput 贯穿全链路**：

```
AgentLoop.Run(ctx, input)
  → execute(ctx, input)
    → Kernel.Run(ctx, input)
      → fsm.Machine.Run(ctx, input)
        → state.Execute(ctx, input)
          → ReactAction / DAGAction:
              msg := input.BuildUserMessage()
              sessionMem.AppendMessage(msg)
```

**改动范围**：

| 层级 | 改动 |
|------|------|
| `core/agent_input.go` | **新增** `AgentInput` 类型 + `BuildUserMessage()` |
| `runtime/agent_loop.go` | `Run(ctx, string)` → `Run(ctx, AgentInput)`；`execute` 透传 |
| `runtime/engine/kernel.go` | `Run(ctx, string)` → `Run(ctx, AgentInput)`；透传到 FSM |
| `runtime/engine/fsm/machine.go` | `Run(ctx, string)` → `Run(ctx, AgentInput)` |
| `runtime/engine/fsm/state.go` | `ReactAction` 中 `NewUserMessage(TextContent{goal})` → `input.BuildUserMessage()` |
| 纯文本路径 | `BuildUserMessage()` → `TextContent`，行为与旧版完全一致 |

---

## 八、主对话模型动态切换

### 9.1 问题

当前 ReAct 主对话使用固化 provider（`config.Model.Provider`）。若用户传入图片，文本模型（如 DeepSeek V3）没有视觉能力，无法"看见"图片。即使 `ImageModel` 配了 GPT-4o，它只用于 MediaDescriber 的描述生成，主对话仍然用文本模型——图片 base64 发过去会报错或无响应。

### 9.2 方案：AgentInput 携带 Provider 覆盖

**不改变 Kernel/ReActAgent 的默认 provider**。当输入包含媒体且配置了对应模型时，`AgentLoop.execute()` 解析出正确的 provider，在执行前注入。

```
AgentLoop.execute(ctx, input)
  │
  ├─ resolveProvider(input, cfg)
  │     input.IsTextOnly() → cfg.Model.Provider      (主模型)
  │     input 含 image + ImageModel → ImageModel.Provider
  │     input 含 audio + AudioModel → AudioModel.Provider
  │
  ├─ planner.SetProvider(resolved)   ← 仅本次执行
  │
  └─ planner.Run(ctx, input)
```

### 9.3 改动点

| 层级 | 改动 |
|------|------|
| `core/agent_input.go` | `AgentInput` 无变化 — Provider 选择是 AgentLoop 层的决策，不污染输入类型 |
| `runtime/agent_loop.go` | `execute()` 中新增 `resolveProvider()` → `planner.SetProvider()` |
| `runtime/engine/kernel.go` | 新增 `SetProvider(p)` → 透传到 `mainReact` + `dagExec` + `reviewer` |
| `runtime/engine/infra/react.go` | 新增 `SetProvider(p)` — 线程安全替换 |
| `runtime/engine/infra/worker.go` | `WorkerAgent` 新增 `SetProvider(p)` |
| `runtime/engine/dag/dag.go` | `DAGTaskExecutor` 新增 `SetProvider(p)` → 透传到所有 worker |
| `runtime/engine/infra/reviewer.go` | `Reviewer` 新增 `SetProvider(p)` |

### 9.4 resolveProvider 逻辑

```go
// AgentInput.Validate() 已禁止混合 image 和 audio，故只需检查第一个媒体类型。
func resolveProvider(input core.AgentInput, cfg config.Config) core.ILMProvider {
    if input.IsTextOnly() {
        return cfg.Model.Provider
    }
    switch input.Media[0].Type {
    case constants.MultiModalTypeImage:
        if cfg.Multimodal.ImageModel != nil {
            return cfg.Multimodal.ImageModel.Provider
        }
    case constants.MultiModalTypeAudio:
        if cfg.Multimodal.AudioModel != nil {
            return cfg.Multimodal.AudioModel.Provider
        }
    }
    return cfg.Model.Provider // fallback
}
```

### 9.5 线程安全

`ReActAgent.provider` 加 `sync.RWMutex`，`SetProvider` 写锁，`ExecuteWithTools` 读锁。DAG executor 同理。

### 9.6 并发考量

AgentLoop 是单会话单线程执行（Run → execute 在 goroutine 内顺序执行），不存在并发 SetProvider 和 ExecuteWithTools 的竞争。但加锁是防御性设计——未来可能有多 worker 并发场景。

---

## 九、Web UI 接入

### 9.1 交互

```
┌───┬─────────────────────────────────────┐
│ @ │  描述这张截图                        │  [→]
└───┴─────────────────────────────────────┘
  │  点击 @
  ▼
┌──────────────┐
│ 📷 图片        │  ← 已配 ImageModel → 可点击
│ 🎵 音频 (不可用)│  ← 未配 AudioModel → 置灰
└──────────────┘
  │  选中"图片"
  ▼
📁 系统文件选择器 accept="image/*"
  │  选中后
  ▼
🖼 screenshot.png 12KB ✕     ← chip，输入框上方，可删除，支持多个
```

**核心规则**：popover 中每个 tab 的可用性由模型配置决定 — 配置了对应模型才开放，否则置灰并标注"(不可用)"。

### 9.2 模型可用性 API

```
GET /api/settings/multimodal
→ { "image_available": true, "audio_available": false }
```

前端页面加载时调用，决定 popover 中各 tab 的启用/置灰状态。

后端实现：读取 `config.Multimodal`，检查 `ImageModel != nil` / `AudioModel != nil`。

### 9.3 数据流

```
前端:
  1. 页面加载 → GET /api/settings/multimodal → 更新 popover tab 状态
  2. 用户 @ 选图片 → FileReader → base64 → chip 预览
  3. 发送 POST /api/chat { goal, media: [{type, base64, filename, mime_type}] }

后端 handleChat:
  1. MediaInput → MediaPreprocessor.Normalize() → MultiModalDetail
  2. AgentInput{Text: goal, Media: [detail]}
  3. AgentLoop.Run(ctx, input)
```

### 9.4 改动文件

| 文件 | 操作 | 说明 |
|------|------|------|
| `server.go` | 修改 | 新增路由 `GET /api/settings/multimodal` |
| `settings.go` | 修改 | 新增 `handleGetMultimodalConfig` |
| `chat.go` | 修改 | `MediaInput` + `ChatRequest.Media`；`handleChat` 媒体管道 |
| `session.go` | 修改 | `StoredMessage` 加 `MediaType`/`MediaPath`/`MimeType` + `addTurnMessage` |
| `index.html` | 修改 | `@` 按钮 + popover(动态tab) + 隐藏 file input + `#media-preview` |
| `chat.js` | 修改 | `multimodalCfg` + `pickMedia`/`renderMediaPreview` + `sendMessage` 发 media |
| `style.css` | 修改 | `.attach-btn` / `.media-popover` / `.media-chip` / `.disabled` 样式 |

---

## 十、业界对比

### OpenClaw

- 图像在重放视图中被替换为 `[image data removed - already processed by model]`
- 最近 3 轮逐字节保留以维持 prompt cache
- 原始会话文件永不修改
- **Compactor 只知道"用户分享过图"，不知道图的内容**

### image-context-cascade

- 三级分层：当前轮完整 base64 → 近期缩略图 → 旧历史 Hash 占位符
- 99.98% 压缩率（1.3MB → 315 字符）
- 纯请求层处理，不生成描述

### 我们 vs 业界

| | OpenClaw | image-context-cascade | 我们 |
|---|---|---|---|
| 旧图像在 Compactor 眼中 | `[image data removed]` | Hash | **自然语言描述** |
| Compactor 摘要质量 | "有过图" | "有过图" | **知道图的具体内容** |
| Extractor 可提取图像事实 | ❌ | ❌ | **✅** |
| 描述成本 | 零 | 零 | 元数据免费 + LLM 可选 |

---

## 十一、新增媒体类型的成本

以新增 `MultiModalTypeScreenRecording` 为例：

| 步骤 | 工作 |
|------|------|
| 1. `constants/enums.go` | 加一行常量 |
| 2. `core/media.go` | 注册 `screenRecordingValidator`（3 个方法） |
| 3. `converter.go` | `mediaContentPart` 加一个 case（或走 image_url） |
| 4. `media_describe.go` | 加一条 prompt |
| 5. `config.go` | 加一行配置 |

**管道本身零改动。**

---

## 十二、实施计划

### Phase 1 — 核心类型 + 预处理（已完成 ✅）

| 文件 | 操作 |
|------|------|
| `core/message.go` | MimeType + Meta + MarshalJSON 剥离 Base64 |
| `core/message_test.go` | 新增 4 个测试 |
| `core/media.go` | **新增**: Validator 接口 + Registry + Preprocessor |
| `core/media_test.go` | **新增**: 14 个测试 |

### Phase 2 — Provider 传输层（已完成 ✅）

| 文件 | 操作 |
|------|------|
| `provider/chat_completions/converter.go` | `convertUserMessage` + `mediaContentPart` + `audioFormat` |
| `provider/chat_completions/converter_test.go` | 新增 6 个多模态转换测试 |

### Phase 3 — 媒体描述 + 持久化（已完成 ✅）

| 文件 | 操作 |
|------|------|
| `memory/media_describe.go` | **新增**: MediaDescriber + BuildMediaMessage |
| `memory/media_describe_test.go` | **新增**: 9 个测试 |
| `memory/media_converter.go` | **新增**: base64 ↔ 文件系统 |
| `memory/media_converter_test.go` | **新增**: 10 个测试 |

### Phase 4 — 配置（已完成 ✅）

| 文件 | 操作 |
|------|------|
| `config/config.go` | `MultimodalConfig` |
| `cmd/common/configfile.go` | image/audio 模型加载/保存 |

### Phase 5 — AgentInput + Web UI（已完成 ✅）

### Phase 6 — 主对话模型动态切换

| 文件 | 操作 |
|------|------|
| `runtime/agent_loop.go` | 新增 `resolveProvider()`；`execute()` 调用 `planner.SetProvider()` |
| `runtime/engine/kernel.go` | 新增 `SetProvider()` → 透传 mainReact + dagExec + reviewer |
| `runtime/engine/infra/react.go` | 新增 `SetProvider()` |
| `runtime/engine/infra/worker.go` | 新增 `SetProvider()` |
| `runtime/engine/dag/dag.go` | 新增 `SetProvider()` |
| `runtime/engine/infra/reviewer.go` | 新增 `SetProvider()` |

---

## 十三、关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 传输编码 | Base64 data URL | OpenAI/DeepSeek API 原生支持 |
| JSONL 存 base64 | 不存，引用外部文件 | 避免 JSONL 膨胀 |
| 元数据建模 | `Meta map[string]any` | 各媒体元数据维度不同，通用桶避免字段膨胀 |
| 描述生成时机 | 消息进入 SessionMemory 时 | Compressor/Extractor/Dreamer 只看到 TextContent |
| 描述层次 | 元数据摘要(免费) + LLM 增强(可选) | AutoDescribe=false 时仍有可用文本 |
| AgentLoop API | `Run(ctx, AgentInput)` | 统一入口，无 side channel；Kernel/FSM 不改 |
| 未注册类型 | 直接拒绝 | 文档类由工具层处理，框架不越界 |
| 视频 | 首帧作为 image | 无原生 ContentPart，后续可扩展多帧 |

---

## 附录：文件清单

```
新增 (6):
├── core/agent_input.go              # AgentInput
├── core/media.go                    # MediaTypeValidator + Preprocessor
├── memory/media_describe.go         # MediaDescriber + BuildMediaMessage
├── memory/media_converter.go        # base64 ↔ 文件系统
├── memory/media_describe_test.go
├── memory/media_converter_test.go
├── core/media_test.go

修改 (11):
├── core/message.go                  # MimeType + Meta + MarshalJSON
├── core/message_test.go             # 序列化测试
├── runtime/agent_loop.go            # Run(ctx, AgentInput)
├── runtime/engine/kernel.go         # Run(ctx, AgentInput)
├── runtime/engine/fsm/machine.go    # Run(ctx, AgentInput)
├── runtime/engine/fsm/state.go      # BuildUserMessage() 替代 NewUserMessage
├── provider/chat_completions/converter.go       # convertUserMessage + mediaContentPart
├── provider/chat_completions/converter_test.go
├── config/config.go                 # MultimodalConfig
├── cmd/common/configfile.go
├── cmd/kugelblitz-ui/*              # Web UI 接入

不改 (4):
├── prompts/llm_prompts.go           # Compressor 零感知
├── memory/compressor.go             # 零感知
├── memory/longterm/extractor.go     # 零感知
├── memory/longterm/dream.go         # 零感知
```
