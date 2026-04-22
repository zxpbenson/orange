# Orange 🍊

**Orange** is an AI-powered SSH Proxy Assistant designed to seamlessly integrate into your daily terminal workflow. It acts as a transparent, drop-in replacement for your standard `ssh` command. 

You can use Orange exactly like you use native SSH. But when you encounter a cryptic error, a complex deployment task, or simply need to analyze system performance, an intelligent AI agent is just a keystroke away.

## ✨ Why Orange?

- **Zero Friction Context**: Stop copying and pasting terminal logs into a web browser. Orange automatically captures the last 8KB of your terminal output and sends it directly to the AI as context.
- **Full SSH Compatibility**: Supports Public Key authentication (via `~/.ssh/id_rsa` or SSH Agent), Interactive Password authentication, custom ports, and standard `user@host` routing (even supporting jump hosts).
- **Secure & Transparent**: Built with security in mind. It uses standard `~/.ssh/known_hosts` verification to protect against MITM attacks. 

## 🚀 Core Capabilities

### 🗣️ The Interactive Assistant
Press **`Ctrl+A`** at any time during an active SSH session to wake up the assistant. The connection to the remote server pauses, allowing you to ask the AI a question (in English, Chinese, etc.). The AI analyzes your recent terminal history and provides a response right in your console.

### ⚡ Safe Command Execution
If the AI determines that a specific command will fix your issue, it will suggest it. By default, Orange intercepts this command and prompts you for approval (`Do you want to execute it? [Y/n]`). If approved, it runs the command on your remote server and streams the output back to you immediately.

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
[Orange] Connected. Press Ctrl+A to ask the AI assistant.
```

### CLI Flags
- `-p <port>`: Specify the remote SSH port (default: 22)
- `-i <identity_file>`: Path to the private key file (default: `~/.ssh/id_rsa` or SSH Agent)
- `--approval-policy`: Set to `always` (prompt before running AI commands) or `never` (run immediately, risky!)

## 📁 Documentation
For an in-depth look at how the TTY interceptor, the LLM module, and the SSH client work together, please read the [Architecture Design Document](doc/architecture.md) which includes detailed Mermaid workflow diagrams.

## 📜 License
MIT License
