# Architecture Design

This document outlines the high-level architecture, core modules, and execution workflows of **Orange**, an AI-powered SSH Proxy Assistant.

## 1. System Overview

Orange sits transparently between the user's local terminal and the remote SSH server. It functions in two primary modes:

1.  **Passthrough Mode (Default):** Standard `stdin`, `stdout`, and `stderr` streams are piped directly to and from the remote SSH session. The application quietly maintains a rolling buffer (`RingBuffer`) of recent terminal outputs.
2.  **Assistant Mode:** Triggered by a specific hotkey (`Ctrl+G`). Orange temporarily intercepts `stdin`, pauses the passthrough, and captures user input for the AI. It then sends the user's prompt, along with the contextual history from the `RingBuffer` and loaded `Skills` (Markdown guides), to an external Large Language Model (LLM).

If the AI suggests a command to resolve an issue, Orange enters an **Approval Workflow** to allow the user to execute the command directly on the remote server safely.

## 2. High-Level Architecture

The following diagram illustrates the primary components and data flow within the Orange application.

```mermaid
graph TD
    subgraph Local Environment
        Terminal[User Terminal]
        Config(config.json)
        SkillsDir((Skills\nMarkdown files))
        MCPServers((External\nMCP Servers))
    end

    subgraph Orange Application
        CLI[CLI Entrypoint & Arg Parser]
        ConfigLoader[Config Module]
        
        subgraph TTY Module
            Interceptor[TTY Interceptor]
            RingBuffer[(RingBuffer\n8KB Context)]
            InputHandler[Input State Machine]
        end

        subgraph LLM Module
            PromptBuilder[Prompt Builder]
            HTTPClient[OpenAI API Client]
            SkillsLoader[Skills Loader]
            MCPClient[MCP JSON-RPC Client]
        end

        subgraph SSH Client Module
            SSHDialer[SSH Connection]
            PTYManager[PTY Manager]
        end
    end

    subgraph Remote Environment
        RemoteServer[Remote Linux Server]
        RemoteShell[Bash / Zsh Session]
    end

    %% Connections
    Terminal <-->|Raw Input / Output| CLI
    CLI --> ConfigLoader
    ConfigLoader -->|Loads| Config
    
    CLI --> Interceptor
    
    Interceptor -->|Reads| InputHandler
    InputHandler -->|Ctrl+G trigger| PromptBuilder
    Interceptor -->|Stores last N bytes| RingBuffer
    
    PromptBuilder -->|Context| RingBuffer
    PromptBuilder -->|System Prompts| SkillsLoader
    SkillsLoader -.->|Reads| SkillsDir
    
    PromptBuilder -->|Tools| MCPClient
    MCPClient <-->|JSON-RPC Stdio| MCPServers
    
    PromptBuilder --> HTTPClient
    HTTPClient <-->|HTTPS| ExternalLLM((External LLM\ne.g., GPT-4o))
    
    HTTPClient -->|AI Response| Interceptor
    InputHandler -->|Approval Y/n| Interceptor
    
    Interceptor <-->|Passthrough| SSHDialer
    SSHDialer --> PTYManager
    PTYManager <-->|Encrypted SSH| RemoteServer
    RemoteServer <--> RemoteShell
```

## 3. Core Modules

### 3.1 Main & Config (`cmd/orange/main.go`, `internal/config/`)
-   **CLI Parsing**: Handles command-line flags (`-p`, `-i`, `--approval-policy`), custom `user@host` routing (including jump host syntax), and graceful connection teardown.
-   **Configuration**: Reads the local `~/.internal/config/orange/config.json` to configure the LLM endpoint, model choice, `skills_dir`, and external `mcp_servers`.

### 3.2 SSH Client (`internal/sshclient/`)
-   **Authentication**: Wraps `golang.org/x/crypto/ssh`. It supports public-key authentication via `SSH_AUTH_SOCK` (SSH Agent) or an explicit identity file (`-i`). If public-key auth fails, it falls back to **Interactive Password Authentication**.
-   **Security**: Manages `known_hosts` verification to protect against MITM attacks. If a host key is unknown, it prompts the user for confirmation before appending it to `~/.ssh/known_hosts`.
-   **PTY Management**: Requests a `xterm-256color` PTY that matches the user's local terminal size.

### 3.3 TTY Interceptor (`internal/tty/`)
-   **Stream Bridging**: The heart of the application. It spawns goroutines to continuously read from the remote `stdout`/`stderr` and write to the local terminal while copying a chunk of the stream to the `RingBuffer`.
-   **Input Handling**: It intercepts local `stdin` rune-by-rune (handling multi-byte UTF-8 encoded characters properly, e.g., Chinese input) to detect the `Ctrl+G` sequence and toggle between Passthrough and Assistant modes.

### 3.4 LLM & MCP Integration (`internal/llm/`)
-   **Prompt Engineering**: Formats requests using the standard OpenAI chat completions schema. It dynamically injects instructions from local Markdown files (`Skills`) to constrain the AI's behavior and formatting rules.
-   **MCP Client**: For advanced tool usage, it spawns child processes defined in `mcp_servers`, connects via `stdio`, and negotiates available tools using the Model Context Protocol (JSON-RPC 2.0). These tools are converted into LLM functions and appended to the prompt.

## 4. Execution Workflows

### 4.1 Connection & Authentication Flow

```mermaid
sequenceDiagram
    participant User
    participant Orange
    participant SSHAgent
    participant RemoteServer

    User->>Orange: Run `./orange user@host`
    Orange->>Orange: Load Config & Parse Args
    Orange->>SSHAgent: Request Signers (if available)
    Orange->>RemoteServer: Initiate SSH Handshake
    
    alt Known Host Verification
        RemoteServer-->>Orange: Host Key Fingerprint
        Orange->>Orange: Check ~/.ssh/known_hosts
        alt Key Unknown
            Orange->>User: Prompt "Continue connecting?"
            User-->>Orange: yes
            Orange->>Orange: Append to known_hosts
        end
    end

    alt Authentication
        Orange->>RemoteServer: Try Public Key (Agent or -i)
        alt Fails
            Orange->>User: Prompt for Password
            User-->>Orange: Enter Password
            Orange->>RemoteServer: Try Password Auth
        end
    end

    RemoteServer-->>Orange: Connection Established
    Orange->>RemoteServer: Request PTY & Shell
    Orange->>User: Set Terminal to Raw Mode
    Orange->>Orange: Start TTY Interceptor
```

### 4.2 Assistant & Command Approval Flow

```mermaid
sequenceDiagram
    participant User
    participant TTYInterceptor
    participant LLMModule
    participant RemoteServer

    User->>TTYInterceptor: Types `ls -la`
    TTYInterceptor->>RemoteServer: Passthrough `ls -la`
    RemoteServer-->>TTYInterceptor: Output (e.g., "Permission denied")
    TTYInterceptor->>User: Print Output
    TTYInterceptor->>TTYInterceptor: Save to RingBuffer

    User->>TTYInterceptor: Presses `Ctrl+G`
    TTYInterceptor->>User: Show Assistant Prompt
    User->>TTYInterceptor: Types "Fix this error"
    
    TTYInterceptor->>LLMModule: Send Prompt + RingBuffer Context
    LLMModule-->>TTYInterceptor: AI Response: "Run `sudo ls -la`"
    
    TTYInterceptor->>User: Print AI Explanation
    
    alt Approval Policy: always
        TTYInterceptor->>User: Prompt "Execute command? [Y/n]"
        User-->>TTYInterceptor: Presses `Y`
        TTYInterceptor->>RemoteServer: Send `sudo ls -la`
        RemoteServer-->>TTYInterceptor: Command Output
        TTYInterceptor->>User: Print Output
    else Approval Policy: never
        TTYInterceptor->>RemoteServer: Auto-execute `sudo ls -la`
        RemoteServer-->>TTYInterceptor: Command Output
        TTYInterceptor->>User: Print Output
    end
```
