# Phase 3: 底层通信多路复用与中断控制 (Multiplexing & Interrupt Control)

## 1. 目标
在 Phase 2 实现自主循环后，单靠 PTY 流进行自动化操作存在隐患：例如执行一个长时间运行的命令（如 `docker pull`），或产生海量输出的命令时，会阻塞当前的 Session，且用户的终端画面会被 Agent 的探针和输出填满。
因此需要通过 `ssh.Client` 开启独立的后台 `Session` 来剥离 Agent 指令流。

## 2. 核心设计与数据结构

- 在 `internal/sshclient/ssh.go` 中提供类似 `ExecuteBackgroundCommand(cmd string) (string, int, error)` 的方法，专门用于 Agent 后台执行无交互或不依赖 PTY 的单次命令。
- 拦截器在解析到 Agent 的 `action: exec_command` 时，不再将其发送给 `i.remoteIn`（用户的 PTY 输入流），而是调用这个新的后台方法。

## 3. 具体修改步骤

### 步骤 3.1: 新增后台会话接口
在 `sshclient/ssh.go` 中，封装一个新的方法，用来在不创建 PTY 的前提下创建独立会话：
```go
func (c *Client) ExecuteSilentCmd(cmd string) (stdout string, stderr string, exitCode int, err error) {
	// session, err := c.client.NewSession()
	// capture CombinedOutput(cmd)
}
```
**注意**：如果是 `cd` 或者环境变更命令，后台 `Session` 的变更默认不会影响前台 PTY。因此需要判断：
- 如果是 `cd` 等环境变量命令：必须注入用户的 PTY。
- 如果是 `ls`、`grep` 等探测命令：可以通过后台 `Session` 静默执行。
- 如果难以区分，可以要求 Agent JSON 输出 `interactive: true/false`，或者干脆由后台执行，然后 Agent 自己维护一个相对环境状态字典。

### 步骤 3.2: 进度 UI 反馈
由于后台执行对用户不可见，我们在拦截器中，需要根据 Agent 的 `thought`，在当前光标位置渲染一个加载动画或进度提示：
`\r\033[33m[Agent 执行中] 正在分析 nginx 配置文件...\033[0m`

### 步骤 3.3: 非阻塞与强行接管
在执行后台任务时，我们需要允许用户通过按下特定组合键（如 `Ctrl+O`，或者在 `i.escState` 状态机里增加对 `Ctrl+C` 的独立监听）强行打断当前正在阻塞等待的 Agent Loop，并将控制权交还给用户的 PTY。

## 4. 交付验收标准
1. Agent 执行一个产生 10MB 输出的 `cat` 命令，用户的终端画面上完全不显示这些输出（静默在后台完成），并由 Agent 总结反馈结果。
2. 在 Agent 尝试分析一个耗时 30 秒的过程时，用户按下 `Ctrl+C` 能够立即中止 Agent 的逻辑循环，恢复到正常的 SSH 会话。
