# Orange 🍊

**Orange** is an AI-powered SSH Proxy Assistant designed to seamlessly integrate into your daily terminal workflow. It acts as a transparent, drop-in replacement for your standard `ssh` command. 

You can use Orange exactly like you use native SSH. But when you encounter a cryptic error, a complex deployment task, or simply need to analyze system performance, an intelligent AI agent is just a keystroke away.

![Screenshot](doc/20260422121308_191_654.png)
## ✨ Why Orange?

- **Zero Friction Context**: Stop copying and pasting terminal logs into a web browser. Orange automatically captures the last 8KB of your terminal output and sends it directly to the AI as context.
- **Full SSH Compatibility**: Supports Public Key authentication (via `~/.ssh/id_rsa` or SSH Agent), Interactive Password authentication, custom ports, and standard `user@host` routing (even supporting jump hosts).
- **Secure & Transparent**: Built with security in mind. It uses standard `~/.ssh/known_hosts` verification to protect against MITM attacks. 

## 🚀 Core Capabilities

### 🗣️ The Interactive Assistant
Press **`Ctrl+G`** at any time during an active SSH session to wake up the assistant. The connection to the remote server pauses, allowing you to ask the AI a question (in English, Chinese, etc.). The AI analyzes your recent terminal history and provides a response right in your console.

The assistant input supports **full readline-style line editing** with the following shortcuts:

| Shortcut | Action |
|---|---|
| `←` / `→` | Move cursor left / right |
| `Home` / `End` | Jump to start / end of line |
| `Ctrl+A` / `Ctrl+E` | Jump to start / end of line |
| `Backspace` | Delete character before cursor |
| `Delete` | Delete character at cursor |
| `Ctrl+W` | Delete previous word |
| `Ctrl+U` | Delete from start to cursor |
| `Ctrl+K` | Delete from cursor to end |

Full UTF-8 support is provided, including correct cursor handling for CJK wide characters.

### ⚡ Standard Assistant Mode (Default)
In default mode, Orange acts as an intelligent proxy. If the AI determines that a command will fix your issue, it suggests it and waits for your approval (`[Y/n]`). If approved, Orange types the command into your terminal for you. **Note:** In this mode, Orange does not track when the command finishes. You will see the output on your screen, but you must press `Ctrl+G` again if you want the AI to analyze the results.

### 🤖 Autonomous Agentic Loop (`--autonomous`)
When started with the `--autonomous` flag, Orange transforms into a fully autonomous agent. Instead of stopping after typing a command, it forms a **Reasoning-Acting-Observation Loop**:
1. The AI decides what commands to run to gather data.
2. Orange blocks and actively tracks the command's execution.
3. Once finished, Orange automatically feeds the output and exit codes back to the AI for analysis.
4. The AI summarizes the findings and presents a final report.

It accomplishes this using a **Dual-Track Execution** system:
- **Background Silent Execution**: For analytical commands (like `cat`, `grep`, `top`), the agent executes them in a hidden background SSH session. Your terminal remains clean.
- **Foreground Interactive Execution**: For commands that change your environment (like `cd`) or require a UI (like `vim`), the agent executes them directly in your active PTY.

### 🛠️ Agent Skills System
Orange supports a `Skills` directory where you can store Markdown files containing custom troubleshooting workflows or company-specific SOPs (e.g., Docker Debugging, Log Analysis). These skills are loaded into the AI's system prompt, strictly guiding its behavior and standardizing the commands it suggests.

### 🔌 MCP (Model Context Protocol) Integration
Supercharge your AI! Orange natively supports external tools using the standardized JSON-RPC 2.0 MCP protocol. You can configure Orange to spawn external binaries (like a custom Golang tool, a Python SQLite reader, or Node.js scripts), parse their available tools, and allow the AI to seamlessly interact with your local environment.

---

## 📖 Getting Started

### Prerequisites
- Go 1.21 or higher
- An OpenAI-compatible API Key (OpenAI, DeepSeek, Anthropic, or local endpoints like Ollama)

### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/zxpbenson/orange.git
   cd orange
   ```

2. Build the project:
   ```bash
   make build
   ```
   *(Cross-compilation is supported: `make build-linux`, `make build-mac`, `make build-windows`)*

3. Setup the default configuration:
   ```bash
   make setup-config
   ```
   This will generate `~/.config/orange/config.json`. **Edit this file** to add your API key.

### Usage

Connect to a server just like SSH:
```bash
./build/orange -p 2022 root@127.0.0.1
./build/orange -i ~/.ssh/my_private_key user@host.com
```

When connected, trigger the AI:
```text
[Orange] Connected. Press Ctrl+G to ask the AI assistant.
```

### CLI Flags
- `-p <port>`: Specify the remote SSH port (default: 22)
- `-i <identity_file>`: Path to the private key file (default: `~/.ssh/id_rsa` or SSH Agent)
- `--approval-policy`: Set to `always` (prompt before running AI commands) or `never` (run immediately, risky!)
- `--autonomous`: Enable the fully autonomous Agentic Loop. The AI will continuously execute commands, analyze outputs, and make decisions until the task is complete.

## 📁 Documentation
For an in-depth look at how the TTY interceptor, the LLM module, and the SSH client work together, please read the [Architecture Design Document](doc/architecture.md) which includes detailed Mermaid workflow diagrams.

## 📜 License
MIT License
