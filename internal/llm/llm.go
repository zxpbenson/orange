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
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
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

// AskAssistant sends a prompt to the configured LLM and returns the response.
func AskAssistant(cfg *config.Config, prompt string) (string, error) {
	systemPrompt := buildSystemPrompt(cfg)

	tools, mcpClients := collectMCPTools(cfg)
	defer func() {
		for _, c := range mcpClients {
			c.Close()
		}
	}()

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	chatResp, err := sendChatRequest(cfg, messages, tools)
	if err != nil {
		return "", err
	}

	return extractResponse(chatResp, cfg.Autonomous)
}

// buildSystemPrompt constructs the system prompt based on mode (autonomous/manual) and appends loaded skills.
func buildSystemPrompt(cfg *config.Config) string {
	prompt := "You are an AI assistant helping a user manage a Linux server via SSH. The user will provide terminal history and a question or instruction.\n\n"

	if cfg.Autonomous {
		prompt += "CRITICAL INSTRUCTION: You are in AUTONOMOUS mode. You MUST output your response ONLY as a valid JSON object matching this schema:\n"
		prompt += "{\n"
		prompt += "  \"thought\": \"Your reasoning about the current state and what to do next\",\n"
		prompt += "  \"action\": \"exec_command\" or \"finish\",\n"
		prompt += "  \"command\": \"The shell command to run (only if action is exec_command)\",\n"
		prompt += "  \"interactive\": true or false, true ONLY if it changes env (cd) or opens UI (vim, top)\n"
		prompt += "  \"status\": \"CONTINUE\" or \"DONE\",\n"
		prompt += "  \"final_answer\": \"Your final response to the user (only if status is DONE)\"\n"
		prompt += "}\n"
		prompt += "Do not include any markdown formatting like ```json, just output the raw JSON object."
	} else {
		prompt += "CRITICAL INSTRUCTION: If you want to execute a command on the user's server, you MUST output it inside a special code block like this:\n"
		prompt += "```bash\n<command>\n```\n"
		prompt += "Do not use any other format for commands you want the user to run."
	}

	// Append skills if available
	prompt += skills.LoadSkills(cfg.SkillsDir)

	return prompt
}

// collectMCPTools starts MCP servers, fetches their tool lists, and converts them to LLM Tool definitions.
// Callers are responsible for closing the returned MCPClients.
func collectMCPTools(cfg *config.Config) ([]Tool, []*MCPClient) {
	var tools []Tool
	var mcpClients []*MCPClient

	for name, srvCfg := range cfg.MCPServers {
		c, err := StartMCP(&srvCfg)
		if err != nil {
			continue
		}
		mcpClients = append(mcpClients, c)

		res, err := c.Call("tools/list", nil)
		if err != nil || res.Result == nil {
			continue
		}

		var listRes struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(res.Result, &listRes); err != nil {
			continue
		}

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

	return tools, mcpClients
}

// sendChatRequest marshals the request, sends it to the LLM API, and returns the decoded response.
func sendChatRequest(cfg *config.Config, messages []Message, tools []Tool) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Model:    cfg.Model,
		Messages: messages,
		Tools:    tools,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", cfg.LLMEndpoint+"/chat/completions", bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("API Error (%d): %s", resp.StatusCode, errResp.Error.Message)
		}
		return nil, fmt.Errorf("API Error (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, err
	}

	return &chatResp, nil
}

// extractResponse extracts the assistant's text content from the API response,
// handling tool call mocking when applicable.
func extractResponse(chatResp *ChatResponse, autonomous bool) (string, error) {
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	msg := chatResp.Choices[0].Message

	if len(msg.ToolCalls) > 0 {
		toolName := msg.ToolCalls[0].Function.Name
		if autonomous {
			return fmt.Sprintf(`{
  "thought": "I am deciding to call the external MCP tool %s to get more information.",
  "action": "finish",
  "status": "DONE",
  "final_answer": "The AI decided to call the tool: %s. (Tool execution mocked)"
}`, toolName, toolName), nil
		}
		return fmt.Sprintf("The AI decided to call the tool: %s. (Tool execution mocked)", toolName), nil
	}

	return msg.Content, nil
}
