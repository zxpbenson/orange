# Phase 2: Agentic Loop 与交互协议闭环 (The Automation Loop)

## 1. 目标
在 Phase 1 已经能够准确感知环境状态的基础上，实现“决策-行动”的自主循环。抛弃原有的自然语言对话模式，让 Agent 吐出结构化 JSON 意图，并由本地拦截器驱动自动执行。

## 2. 核心设计与数据结构

引入 `Orange-Protocol` 交互契约：
```json
{
    "thought": "分析当前处境和下一步的意图",
    "action": "exec_command",
    "command": "sudo systemctl restart nginx",
    "status": "CONTINUE"
}
```
如果遇到需要用户介入或者任务完成的情况：
```json
{
    "thought": "任务完成。已经重启了 Nginx。",
    "action": "finish",
    "status": "DONE",
    "final_answer": "Nginx 已成功重启，当前状态为 active。"
}
```

## 3. 具体修改步骤

### 步骤 2.1: 升级 LLM Prompt 强制 JSON 输出
修改 `llm.AskAssistant` 的 System Prompt，要求 LLM **只能** 以上述 JSON 格式返回响应。在提示词中详细说明 `thought`, `action`, `command`, `status` 的字段含义。

### 步骤 2.2: 引入 `--autonomous` 模式标志
在 `cmd/orange/main.go` 中增加启动参数标志 `--autonomous`（例如默认为 false）。当此模式开启时，不再向终端输出 `[Orange] AI suggests running this command... Do you want to execute it? [Y/n]`。

### 步骤 2.3: 编写 Agent Loop
在 `internal/tty/tty.go` 中，当触发快捷键且处理 LLM 响应时，不再直接 break 退出 Assistant 模式。
而是进入一个小的 `for` 循环：
1. 解析返回的 JSON。
2. 如果 `status == "DONE"`，打印 `final_answer` 并退出 Assistant 模式。
3. 如果 `status == "CONTINUE"` 且 `action == "exec_command"`：
   - 拦截器自动将该 `command` (带上 Phase 1 的探针) 下发至远端。
   - 阻塞等待探针返回退出码和输出（设定超时机制）。
   - 将新的结果拼接为 `SessionContext`，再次静默请求 LLM（`AskAssistant`），跳回第 1 步。

## 4. 交付验收标准
用户提出一个需要两步的操作（如：“找到 80 端口被哪个进程占用并 kill 掉”）。Agent 会循环两次：
第一次：输出 `netstat` 或 `lsof`。
系统自动执行并回传结果给 Agent。
第二次：Agent 根据输出得到 PID，输出 `kill -9 <PID>`。
系统自动执行完毕后，Agent 返回 `status: DONE` 并向用户汇报。整个过程除提示外无需用户干预。
