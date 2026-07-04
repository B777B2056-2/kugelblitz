# CLAUDE.md – Golang Agent 框架

本文档为参与此 Go Agent 框架项目的 AI 助手（如 Claude）提供开发指导。项目**强制**采用测试驱动开发（TDD）作为核心实践。

---

## 项目概述

**目标** – 构建一个轻量、模块化的 Agent 框架，用于执行监控、数据采集、事件处理和远程控制等任务。框架提供：

- 可插拔的输入/输出适配器
- 配置管理
- 生命周期钩子（启动、停止、重载）
- 并发任务调度
- 遥测与日志

**技术栈** – Go 1.26+，使用 `go.mod` 管理依赖，主要依赖标准库，测试使用 `testify`。

---

## 开发环境

- **Go 版本** – 1.26 或更高（具体版本请查看 `go.mod`）
- **依赖管理** – Go Modules，使用 `go mod tidy` 同步依赖
- **代码检查** – `golangci-lint`（配置文件 `.golangci.yml`）
- **测试** – `go test` 配合 `-race` 标志，覆盖率通过 `go test -cover` 生成

---

## TDD 工作流（强制要求）

所有功能开发、缺陷修复或重构**必须**遵循 **红‑绿‑重构** 循环：

1. **红** – 编写一个失败的测试，明确定义期望的行为。
2. **绿** – 编写最少的代码使测试通过。
3. **重构** – 在保持所有测试通过的前提下，改善代码结构、可读性和性能。

### 编写测试

- 使用标准库 `testing` 和 `testify` 进行断言：
  ```go
  import (
      "testing"
      "github.com/stretchr/testify/assert"
      "github.com/stretchr/testify/require"
  )
