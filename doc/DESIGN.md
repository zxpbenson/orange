# Orange: AI-powered SSH Proxy Assistant

## 整体流程 (Overall Flow)
1. **启动与配置加载**: 用户在本地运行 `orange user@host`，程序首先读取本地配置文件（如 `~/.config/orange/config.yaml`），获取 LLM API Key、Model 服务地址等。
2. **建立 SSH 连接**: 建立到目标 Linux 服务器的 SSH 连接，并请求一个 PTY（伪终端）。
3. **输入拦截与分发**:
   - 将本地终端设置为 Raw 模式。
   - 监听用户的键盘输入。
   - **直通模式 (Passthrough)**: 普通输入直接转发给远端 SSH 服务器，远端输出直接打印到本地终端，同时在内存中维护一段滚动日志（Context History），记录最近的命令和输出。
   - **助手模式 (Assistant)**: 用户输入特定的唤醒词（例如以 `/?` 开头，或者按下特定的快捷键如 `Ctrl+G`），进入助手交互模式。
4. **AI 协助**:
   - 收集用户的自然语言指令（如“排查一下为什么 nginx 没启动”）。
   - 将终端的历史上下文和用户指令组装成 Prompt。
   - 调用配置好的 LLM 服务。
   - （可选）解析 LLM 返回的建议命令，甚至允许用户按回车直接执行。

## 架构与模块分工 (Architecture & Modules)

### 1. Config 模块 (config/)
- 负责管理本地配置。
- 支持 YAML 格式。
- 包含配置项：LLM Endpoint URL, API Key, Model Name, SSH 默认私钥路径等。

### 2. SSH 模块 (sshclient/)
- 封装 `golang.org/x/crypto/ssh`。
- 处理公钥认证、密码认证。
- 创建 Session、请求 PTY、处理窗口大小改变（Window Change）信号。

### 3. TTY 拦截与状态机模块 (tty/)
- 核心模块。负责将本地 `os.Stdin` 和 `os.Stdout` 与 SSH Session 桥接。
- 维护一个 Ring Buffer，记录最近 N 行的输入输出历史。
- 状态机：判断当前是处在“普通 SSH 模式”还是“助手输入模式”。

### 4. LLM 交互模块 (llm/)
- 封装对 OpenAI 兼容 API（或特定模型 API）的 HTTP 请求。
- 根据终端历史构建上下文 Prompt。
- 处理流式输出 (Streaming) 以获得更好的用户体验。

### 5. Main / CLI 组装 (main.go)
- 使用 `spf13/cobra` 或标准库解析命令行参数。
- 初始化并桥接上述所有模块。

