package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/zxpbenson/orange/internal/config"
	"github.com/zxpbenson/orange/internal/llm/skills"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func AskAssistant(cfg *config.Config, prompt string) (string, error) {
	systemPrompt := "You are an AI assistant helping a user manage a Linux server via SSH. The user will provide terminal history and a question or instruction.\n\n"
	if cfg.Autonomous {
		systemPrompt += "CRITICAL INSTRUCTION: You are in AUTONOMOUS mode. You MUST output your response ONLY as a valid JSON object matching this schema:\n"
		systemPrompt += "{\n"
		systemPrompt += "  \"thought\": \"Your reasoning about the current state and what to do next\",\n"
		systemPrompt += "  \"action\": \"exec_command\" or \"finish\",\n"
		systemPrompt += "  \"command\": \"The shell command to run (only if action is exec_command)\",\n"
		systemPrompt += "  \"interactive\": true or false, true ONLY if it changes env (cd) or opens UI (vim, top)\n"
		systemPrompt += "  \"status\": \"CONTINUE\" or \"DONE\",\n"
		systemPrompt += "  \"final_answer\": \"Your final response to the user (only if status is DONE)\"\n"
		systemPrompt += "}\n"
		systemPrompt += "Do not include any markdown formatting like ```json, just output the raw JSON object."
	} else {
		systemPrompt += "CRITICAL INSTRUCTION: If you want to execute a command on the user's server, you MUST output it inside a special code block like this:\n"
		systemPrompt += "```bash\n<command>\n```\n"
		systemPrompt += "Do not use any other format for commands you want the user to run."
	}

	// Append skills if available
	systemPrompt += skills.LoadSkills(cfg.SkillsDir)

	var tools []Tool
	var mcpClients []*MCPClient
	defer func() {
		for _, c := range mcpClients {
			c.Close()
		}
	}()

	for name, srvCfg := range cfg.MCPServers {
		// Start MCP Client
		c, err := StartMCP(&srvCfg)
		if err != nil {
			continue // skip on failure
		}
		mcpClients = append(mcpClients, c)

		// Fetch tools from MCP
		res, err := c.Call("tools/list", nil)
		if err == nil && res.Result != nil {
			var listRes struct {
				Tools []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"tools"`
			}
			json.Unmarshal(res.Result, &listRes)
			for _, t := range listRes.Tools {
				tools = append(tools, Tool{
					Type: "function",
					Function: struct {
						Name        string      `json:"name"`
						Description string      `json:"description"`
						Parameters  interface{} `json:"parameters"`
					}{
						Name:        fmt.Sprintf("mcp_%s_%s", name, t.Name),
						Description: t.Description,
						Parameters: map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				})
			}
		}
	}

	reqBody := ChatRequest{
		Model: cfg.Model,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		Tools: tools,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", cfg.LLMEndpoint+"/chat/completions", bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var errResp struct { Error struct { Message string `json:"message"` } `json:"error"` }; if err := json.Unmarshal(bodyBytes, &errResp); err == nil && errResp.Error.Message != "" { return "", fmt.Errorf("API Error (%d): %s", resp.StatusCode, errResp.Error.Message) }; return "", fmt.Errorf("API Error (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}

	if len(chatResp.Choices) > 0 {
		msg := chatResp.Choices[0].Message
		
		// Basic handling of tool call (Mock return)
		if len(msg.ToolCalls) > 0 {
			if cfg.Autonomous {
				// Return a valid JSON to satisfy the Agentic Loop
				return fmt.Sprintf(`{
  "thought": "I am deciding to call the external MCP tool %s to get more information.",
  "action": "finish",
  "status": "DONE",
  "final_answer": "The AI decided to call the tool: %s. (Tool execution mocked)"
}`, msg.ToolCalls[0].Function.Name, msg.ToolCalls[0].Function.Name), nil
			}
			return fmt.Sprintf("The AI decided to call the tool: %s. (Tool execution mocked)", msg.ToolCalls[0].Function.Name), nil
		}

		return msg.Content, nil
	}

	return "", fmt.Errorf("no response choices returned")
}
