# Orange 架构升级规划：从 SSH 代理到自主智能体 (Autonomous Agent)

## 核心目标
将 Orange 从单轮对话的“指令中转站”升级为具备 **感知 (Perception) - 决策 (Planning) - 执行 (Action)** 闭环的复杂任务编排中心。

## 阶段一：感知层升级 - 影子会话 (Shadow Session) 与上下文记忆
目前 IO 层仅通过 RingBuffer 被动记录终端输出。需要升级为结构化的状态机。
- **SessionContext 维护**：在 IO 层维护当前会话的真实状态（如当前路径、环境变量）。
- **执行结果捕获**：Agent 执行命令后，精准捕获该命令的 `Stdout/Stderr` 和 `Exit Code`（例如通过注入 `echo $?`），让 Agent 明确知道上一步的真实执行结果。
- **流式过滤**：在本地对大日志进行流式截断和过滤，避免将无效长文本发送给 LLM，优化内存和 Token 消耗。

## 阶段二：决策层升级 - 任务编排引擎 (Orchestration Engine)
引入位于 Agent 之上的逻辑层，负责将自然语言的复杂任务拆解为可执行的子任务。
- **Planner (规划器)**：接收用户输入，输出结构化的 `TaskTree`（JSON 格式的任务序列）。
- **Executor (执行器)**：逐条执行 `TaskTree` 中的 Action。
- **Re-evaluator (复评器)**：每步执行完后，判断结果是否符合预期。如果不符，触发 Re-planning（重新规划）。
- **代码落地**：在 `internal/agent` 目录下新增 `workflow.go`，定义 `Step` 结构体（包含 `Command`, `ExpectedOutput`, `RetryStrategy`）。

## 阶段三：执行层升级 - 自主循环与协议 (Agentic Loop & Protocol)
打破“单次请求-单次返回”的限制，实现真正的连续执行。
- **Orange-Protocol**：定义一套 Agent 与执行层交互的 JSON 协议。Agent 不再直接输出纯文本 Shell，而是输出意图：
  ```json
  {
    "thought": "需要检查 Nginx 配置文件是否有语法错误",
    "action": "exec",
    "command": "nginx -t",
    "status": "CONTINUE"
  }
  ```
- **For-Loop 决策机制**：拦截器解析到 `action: exec` 时，自动下发命令，捕获输出并静默回传给 LLM，直到 LLM 返回 `{"status": "FINISHED"}`。
- **工具箱映射 (Function Calling)**：结合现有的 MCP 架构，将 `read_file`、`list_processes` 等高频操作封装为标准工具，由 Agent 调度，提升执行稳定性。

## 阶段四：底层通信升级 - 多路复用与中断控制
复杂任务涉及耗时操作，必须保证底层 SSH 连接的稳定性和用户体验。
- **独立 SSH Channel**：Agent 的自动化指令不再混入用户的 PTY 数据流，而是通过 `ssh.Client` 开启独立的后台 `Session` 执行。实现数据流的物理隔离，防止字符冲突。
- **非阻塞模式切换**：允许用户在 Agent 自动执行时，通过特定快捷键（如 `Ctrl+O`）强行打断 Agent，接管当前终端控制权。
- **UI 进度反馈**：在用户的前台 PTY 中渲染 Agent 的执行进度条（如 `[1/3] 正在检查依赖...`），保持过程透明。

## 实施建议
建议从 **阶段三（自主循环与协议）** 的核心 Loop 开始验证，先在 `tty.go` 中引入 `--autonomous` 模式，跑通一个简单的多步连续执行 Demo，再逐步向外扩展编排引擎和底层通道分离。
