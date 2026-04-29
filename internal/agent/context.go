package agent

type SessionContext struct {
	LastCommand  string
	LastOutput   string
	LastExitCode string // store as string, easier to parse from bash output
	CurrentDir   string
}

// GetContextPrompt formats the current execution context to be included in the LLM prompt.
func (c *SessionContext) GetContextPrompt() string {
	if c.LastCommand == "" {
		return "No commands executed yet."
	}

	prompt := "【Previous Execution Context】\n"
	prompt += "Command: " + c.LastCommand + "\n"
	prompt += "Exit Code: " + c.LastExitCode + "\n"
	if c.CurrentDir != "" {
		prompt += "Current Dir: " + c.CurrentDir + "\n"
	}
	
	out := c.LastOutput
	if len(out) > 2000 {
		out = out[:2000] + "...[Output Truncated]..."
	}
	prompt += "Output:\n" + out + "\n"
	return prompt
}
