package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type MCPServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env"`
}

type Config struct {
	LLMEndpoint  string                     `json:"llm_endpoint"`
	APIKey       string                     `json:"api_key"`
	Model        string                     `json:"model"`
	SkillsDir    string                     `json:"skills_dir"`
	MCPServers   map[string]MCPServerConfig `json:"mcp_servers"`
	ShortcutKey  string                     `json:"shortcut_key"`
	ApprovalMode string                     `json:"-"` // Set via CLI flag, not JSON
	Autonomous   bool                       `json:"-"` // Set via CLI flag
}

func LoadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, ".config", "orange", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Return default config if file does not exist
		if os.IsNotExist(err) {
			return &Config{
				LLMEndpoint: "https://api.openai.com/v1",
				Model:       "gpt-4o",
				ShortcutKey: "ctrl+g",
			}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.ShortcutKey == "" {
		cfg.ShortcutKey = "ctrl+g"
	}

	return &cfg, nil
}
