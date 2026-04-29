# Phase 3 (Revised): 底层通信多路复用与中断控制

## 1. 重新评估与挑战
原有的 Phase 3 规划虽然方向正确（使用独立的后台 SSH Session 执行 Agent 命令），但在工程实现上存在几个巨大的挑战，需要重新细化：

1. **环境隔离问题**：每次调用 `client.NewSession()` 都会创建一个全新的、干净的 Shell 环境。它**不会**继承用户当前前台 PTY 的工作目录 (`$PWD`) 和临时环境变量。如果用户在前台 `cd /var/log`，然后唤醒 Agent 执行 `ls`，后台 Session 默认会在 `/root` 下执行，导致结果完全错误。
2. **状态同步问题**：如果 Agent 决定执行 `cd /etc/nginx`，这个命令如果放在后台 Session 执行，执行完 Session 就销毁了，用户的前台 PTY 根本不会切换目录。
3. **探针的去留**：一旦我们使用后台 Session，我们就不再需要像 Phase 1 那样向用户的屏幕注入恶心的 `echo "--ORANGE_START--"` 探针了，因为 `session.CombinedOutput(cmd)` 原生就能返回干净的输出和 Exit Code。

## 2. 拆解后的具体实施步骤

### 步骤 3.1: 改造 SSH Client，支持带上下文的后台执行
在 `internal/sshclient/ssh.go` 中新增方法：
```go
func (c *Client) ExecuteBackground(cmd string, workDir string) (string, int, error)
```
- **实现逻辑**：创建一个新的 `Session`。如果传入了 `workDir`，则将命令改写为 `cd <workDir> && <cmd>`，然后调用 `session.CombinedOutput()`。
- **获取 Exit Code**：解析返回的 `error`，如果是 `*ssh.ExitError`，则提取其 `ExitStatus()`。

### 步骤 3.2: 引入 `interactive` 意图字段
修改 `internal/llm/llm.go` 的 Prompt 和 `internal/agent/protocol.go` 的 JSON 结构，增加一个布尔字段 `interactive`。
- **Prompt 约束**：告诉 LLM：“如果你要执行的命令会改变用户的环境（如 `cd`, `export`, `source`），或者需要打开交互式 UI（如 `vim`, `top`, `less`），请将 `interactive` 设为 `true`。如果是普通的查询、分析、修改文件（如 `ls`, `cat`, `grep`, `systemctl`），请设为 `false`。”

### 步骤 3.3: 拦截器的双轨执行逻辑
修改 `internal/tty/tty.go` 中的 Agent Loop：
- **当 `interactive == false` (后台静默执行)**：
  1. 打印 `[Agent Executing in Background] <cmd>`。
  2. 调用 `ExecuteBackground(cmd, i.ctx.CurrentDir)`。
  3. 直接拿到干净的 Output 和 Exit Code，更新 `SessionContext`。
  4. **不再需要等待 `recordDone` 通道**，直接进入下一轮 LLM 思考。
- **当 `interactive == true` (前台注入执行)**：
  1. 像 Phase 1/2 一样，把命令注入到 `i.remoteIn`。
  2. 依然使用探针和 `recordDone` 通道阻塞等待，确保用户的 PTY 状态被真实改变。

### 步骤 3.4: 优雅的中断控制 (Ctrl+C)
在后台执行或 LLM 思考期间，主 Goroutine 处于阻塞状态。我们需要在读取用户输入的 Goroutine 中，如果检测到 `0x03` (Ctrl+C)，则向一个全局的 `cancelChan` 发送信号，强行打断当前的 Agent Loop，恢复到普通 SSH 模式。

## 3. 为什么这样设计？
这种“双轨制”完美解决了环境隔离和屏幕污染的问题。90% 的分析命令（`cat`, `grep`, `ps`）都在后台瞬间完成，用户的屏幕干干净净；而真正需要改变环境的命令（`cd`）则老老实实在前台执行。这才是真正的“毛坯房”到“精装房”的蜕变。
