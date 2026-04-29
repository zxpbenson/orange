package agent

import (
	"encoding/json"
	"strings"
)

// AgentResponse represents the expected JSON response from the LLM in autonomous mode
type AgentResponse struct {
	Thought     string `json:"thought"`
	Action      string `json:"action"`
	Command     string `json:"command,omitempty"`
	Interactive bool   `json:"interactive"`
	Status      string `json:"status"`
	FinalAnswer string `json:"final_answer,omitempty"`
}

// ParseAgentResponse parses the raw JSON string from LLM into the AgentResponse struct.
func ParseAgentResponse(raw string) (*AgentResponse, error) {
	// Try to clean up the raw string in case the LLM wrapped it in markdown code blocks anyway
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```json") {
		raw = raw[7:]
	} else if strings.HasPrefix(raw, "```") {
		raw = raw[3:]
	}
	if strings.HasSuffix(raw, "```") {
		raw = raw[:len(raw)-3]
	}

	var resp AgentResponse
	err := json.Unmarshal([]byte(raw), &resp)
	return &resp, err
}
