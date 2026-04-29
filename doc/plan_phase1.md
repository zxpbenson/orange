# Phase 1: 感知层与上下文记忆升级 (Observation & Context Layer)

## 1. 目标
将目前 `internal/tty/tty.go` 中基于被动 `RingBuffer` 的简单历史记录，升级为具备命令边界感知和执行状态追踪的 `SessionContext`，为后续的连续自动化执行提供可靠的“环境事实”。

## 2. 核心设计与数据结构

新增模块 `internal/agent/context.go`，定义环境上下文：
```go
type SessionContext struct {
	History      *tty.RingBuffer // 保留原始屏幕输出流用于兜底
	LastCommand  string          // 上一条执行的命令
	LastOutput   string          // 上一条命令的具体输出
	LastExitCode int             // 上一条命令的退出码
	CurrentDir   string          // 当前工作目录 (PWD)
}
```

## 3. 具体修改步骤

### 步骤 1.1: 引入隐式探针 (Invisible Probes)
由于纯 TTY 流很难区分“命令输入”和“命令输出”，我们需要在执行命令时，自动向远端注入探针（如特殊的 echo 边界符）。
- 修改 `tty.go`：当代理拦截到要执行的命令 `cmd` 时，将其改写为：
  `echo "--ORANGE_START--"; cmd; echo "--ORANGE_EXIT_CODE:$?--"`
- 这样在读取 `remoteOut` 时，我们可以通过正则或字符串匹配，精准截取出该命令的实际输出流和退出状态码。

### 步骤 1.2: 日志截断与过滤机制
防止 Agent 读取大文件导致 LLM Token 爆满。
- 在 `SessionContext.LastOutput` 赋值前增加过滤器：如果输出超过一定行数（如 200 行）或大小（如 4KB），自动截断并追加提示 `...[Output Truncated]...`。

### 步骤 1.3: 更新 LLM Prompt
修改 `internal/llm/llm.go` 中的提示词组装逻辑，将 `SessionContext` 的结构化信息注入 Prompt：
```text
【当前环境状态】
最后执行命令: {LastCommand}
退出状态码: {LastExitCode}
命令输出:
{LastOutput}

【最近屏幕历史】
{History}
```

## 4. 交付验收标准
启动 Orange 并连接服务器，执行一个报错的命令（如 `ls /non_exist`），唤醒 Agent，Agent 能够明确说出该命令的退出码不是 0，且确切知道报错信息。
