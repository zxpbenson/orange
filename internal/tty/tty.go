package tty

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/zxpbenson/orange/internal/agent"
	"github.com/zxpbenson/orange/internal/config"
	"github.com/zxpbenson/orange/internal/llm"
	"github.com/zxpbenson/orange/internal/sshclient"
)

// RingBuffer stores the last N bytes of terminal output
type RingBuffer struct {
	buf []byte
	mu  sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf: make([]byte, 0, size),
	}
}

func (r *RingBuffer) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Simple append and truncate for now
	r.buf = append(r.buf, p...)
	maxSize := cap(r.buf)
	if len(r.buf) > maxSize {
		r.buf = r.buf[len(r.buf)-maxSize:]
	}
	return len(p), nil
}

func (r *RingBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// Interceptor handles the bridging between local stdin/stdout and remote ssh session
type Interceptor struct {
	cfg          *config.Config
	sshClient    *sshclient.Client
	remoteIn     io.Writer
	remoteOut    io.Reader
	remoteErr    io.Reader
	history      *RingBuffer
	assistant    bool
	lineEditor   *LineEditor
	pendingCmd   string
	awaitingConf bool
	escState     int    // 0: normal, 1: seen ESC, 2: in OSC, 3: in OSC seen ESC, 4: in CSI
	csiParams    []byte // accumulated CSI parameter bytes (state 4)
	ctx          *agent.SessionContext
	recording    bool
	recordBuf    bytes.Buffer
	recordDone   chan bool
}

func NewInterceptor(cfg *config.Config, remoteIn io.Writer, remoteOut, remoteErr io.Reader) *Interceptor {
	return &Interceptor{
		cfg:        cfg,
		sshClient:  nil, // Will set later
		remoteIn:   remoteIn,
		remoteOut:  remoteOut,
		remoteErr:  remoteErr,
		history:    NewRingBuffer(8192), // Keep last 8KB of output
		lineEditor: NewLineEditor(os.Stdout),
		ctx:        &agent.SessionContext{},
		recordDone: make(chan bool, 1),
	}
}

func (i *Interceptor) SetClient(client *sshclient.Client) {
	i.sshClient = client
}

func parseShortcutKey(key string) byte {
	key = strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(key, "ctrl+") && len(key) == 6 {
		char := key[5]
		if char >= 'a' && char <= 'z' {
			return char - 'a' + 1
		}
	}
	return 0x07 // fallback to ctrl+g
}

func extractCommand(text string) string {
	start := strings.Index(text, "```bash")
	if start != -1 {
		start += 7
		end := strings.Index(text[start:], "```")
		if end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	return ""
}

// Start launches the I/O bridge between local terminal and remote SSH session.
func (i *Interceptor) Start() {
	go i.handleRemoteOutput()
	go i.handleRemoteError()

	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}
		i.handleLocalInput(buf[:n])
	}
}

// handleRemoteOutput reads remote stdout, filters ORANGE markers, writes to local terminal and history.
func (i *Interceptor) handleRemoteOutput() {
	buf := make([]byte, 1024)
	for {
		n, err := i.remoteOut.Read(buf)
		if n > 0 {
			outputStr := string(buf[:n])
			if strings.Contains(outputStr, "--ORANGE_") {
				// Line-by-line filter to avoid printing probe commands and outputs
				lines := strings.Split(outputStr, "\n")
				var filtered []string
				for _, line := range lines {
					if !strings.Contains(line, "--ORANGE_") {
						filtered = append(filtered, line)
					}
				}
				filteredStr := strings.Join(filtered, "\n")
				os.Stdout.Write([]byte(filteredStr))
				i.history.Write([]byte(filteredStr))
			} else {
				os.Stdout.Write(buf[:n])
				i.history.Write(buf[:n])
			}

			if i.recording {
				i.processRecordingOutput(buf[:n])
			}
		}
		if err != nil {
			break
		}
	}
}

// processRecordingOutput parses recording buffer for ORANGE markers to extract exit code, pwd, and command output.
func (i *Interceptor) processRecordingOutput(data []byte) {
	i.recordBuf.Write(data)
	output := i.recordBuf.String()

	exitMarkerIdx := strings.Index(output, "--ORANGE_EXIT_CODE:")
	if exitMarkerIdx == -1 {
		return
	}

	endIdx := strings.Index(output[exitMarkerIdx+19:], "--")
	if endIdx == -1 {
		return
	}

	i.ctx.LastExitCode = output[exitMarkerIdx+19 : exitMarkerIdx+19+endIdx]

	// Extract PWD if present
	pwdMarkerIdx := strings.Index(output, "--ORANGE_PWD:")
	if pwdMarkerIdx != -1 {
		pwdEndIdx := strings.Index(output[pwdMarkerIdx+13:], "--")
		if pwdEndIdx != -1 {
			i.ctx.CurrentDir = output[pwdMarkerIdx+13 : pwdMarkerIdx+13+pwdEndIdx]
		}
	}

	// Clean up the output: extract content between START and EXIT_CODE markers
	cleanOut := output[:exitMarkerIdx]
	startMarker := "--ORANGE_START--"
	startIdx := strings.Index(cleanOut, startMarker)
	if startIdx != -1 {
		cleanOut = cleanOut[startIdx+len(startMarker):]
		if strings.HasPrefix(cleanOut, "\r\n") {
			cleanOut = cleanOut[2:]
		} else if strings.HasPrefix(cleanOut, "\n") {
			cleanOut = cleanOut[1:]
		}
	}
	i.ctx.LastOutput = cleanOut

	// Reset recording state and signal completion
	i.recording = false
	i.recordBuf.Reset()
	select {
	case i.recordDone <- true:
	default:
	}
}

// handleRemoteError reads remote stderr and writes to local stderr and history.
func (i *Interceptor) handleRemoteError() {
	buf := make([]byte, 1024)
	for {
		n, err := i.remoteErr.Read(buf)
		if n > 0 {
			os.Stderr.Write(buf[:n])
			i.history.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}

// handleLocalInput dispatches local input based on current state:
// confirmation mode, assistant mode, or normal SSH passthrough.
func (i *Interceptor) handleLocalInput(inputData []byte) {
	// If waiting for Y/n confirmation
	if i.awaitingConf {
		i.handleConfirmation(inputData[0])
		return
	}

	// Run ESC state machine and check for shortcut key
	inputData = i.filterInputForShortcut(inputData)
	if len(inputData) == 0 {
		return
	}

	if i.assistant {
		i.processAssistantInput(inputData)
	} else {
		// Normal SSH passthrough
		i.remoteIn.Write(inputData)
	}
}

// handleConfirmation processes Y/n input when a command is pending approval.
func (i *Interceptor) handleConfirmation(char byte) {
	if char == 'y' || char == 'Y' || char == '\r' || char == '\n' {
		fmt.Print("\r\n\033[32m[Orange] Executing...\033[0m\r\n")
		i.ctx.LastCommand = i.pendingCmd
		i.remoteIn.Write([]byte(i.pendingCmd + "\n"))
	} else {
		fmt.Print("\r\n\033[31m[Orange] Cancelled.\033[0m\r\n")
		i.remoteIn.Write([]byte{'\r'})
	}
	i.awaitingConf = false
}

// filterInputForShortcut processes ESC sequences and detects the assistant shortcut key.
// In assistant mode, CSI sequences (arrow keys, Home/End, Delete) are intercepted and
// dispatched to the LineEditor. In passthrough mode, all bytes flow through untouched.
// Returns the input data with shortcut key bytes and consumed CSI sequences removed.
func (i *Interceptor) filterInputForShortcut(data []byte) []byte {
	shortcutByte := parseShortcutKey(i.cfg.ShortcutKey)

	for j := 0; j < len(data); j++ {
		char := data[j]

		switch i.escState {
		case 0: // normal
			if char == '\x1b' {
				i.escState = 1
				if i.assistant {
					// In assistant mode, consume the ESC byte (it's part of a CSI/OSC sequence)
					data = append(data[:j], data[j+1:]...)
					j--
				}
			} else if char == shortcutByte {
				i.assistant = !i.assistant
				if i.assistant {
					fmt.Print("\r\n\033[33m[Orange Assistant] Enter your question (press Enter to submit, Ctrl+C to cancel): \033[0m")
					i.lineEditor.Reset()
				} else {
					fmt.Print("\r\n\033[32m[Orange] Returned to SSH.\033[0m\r\n")
				}
				data = append(data[:j], data[j+1:]...)
				j--
			}

		case 1: // seen ESC
			if char == '[' && i.assistant {
				// CSI sequence in assistant mode — consume and enter state 4
				i.escState = 4
				i.csiParams = i.csiParams[:0]
				data = append(data[:j], data[j+1:]...)
				j--
			} else if char == ']' {
				i.escState = 2
			} else if char == '\x1b' {
				i.escState = 1
				if i.assistant {
					data = append(data[:j], data[j+1:]...)
					j--
				}
			} else {
				i.escState = 0
			}

		case 2: // in OSC
			if char == '\x07' {
				i.escState = 0
			} else if char == '\x1b' {
				i.escState = 3
			}

		case 3: // in OSC seen ESC
			if char == '\\' {
				i.escState = 0
			} else if char == '\x1b' {
				i.escState = 3
			} else {
				i.escState = 2
			}

		case 4: // in CSI (assistant mode only)
			// Consume all CSI bytes from the data stream
			data = append(data[:j], data[j+1:]...)
			j--

			if char >= 0x20 && char <= 0x3F {
				// Parameter or intermediate byte — accumulate
				i.csiParams = append(i.csiParams, char)
			} else if char >= 0x40 && char <= 0x7E {
				// Final byte — dispatch the CSI command
				i.dispatchCSI(char)
				i.escState = 0
			} else {
				// Malformed — discard
				i.escState = 0
			}
		}
	}

	return data
}

// dispatchCSI handles a complete CSI sequence in assistant mode.
func (i *Interceptor) dispatchCSI(final byte) {
	param := string(i.csiParams)
	switch final {
	case 'C': // Right arrow
		i.lineEditor.MoveRight()
	case 'D': // Left arrow
		i.lineEditor.MoveLeft()
	case 'H': // Home
		i.lineEditor.Home()
	case 'F': // End
		i.lineEditor.End()
	case '~':
		switch param {
		case "3": // Delete
			i.lineEditor.Delete()
		case "1": // Home (alternate)
			i.lineEditor.Home()
		case "4": // End (alternate)
			i.lineEditor.End()
		}
	case 'A', 'B': // Up/Down arrow — ignore for now (future: history)
	}
}

// processAssistantInput handles input in assistant mode, delegating editing
// operations to the LineEditor. CSI sequences (arrows, Home/End, Delete) are
// already consumed by filterInputForShortcut; only printable runes and control
// characters arrive here.
func (i *Interceptor) processAssistantInput(data []byte) {
	rest := data
	for len(rest) > 0 {
		r, size := utf8.DecodeRune(rest)
		if r == utf8.RuneError && size == 1 {
			// Invalid or incomplete byte — skip
			rest = rest[1:]
			continue
		}

		switch {
		case r == '\r' || r == '\n':
			i.submitQuestion(i.lineEditor.String())
			return

		case r == 0x03: // Ctrl+C
			i.assistant = false
			fmt.Print("\r\n\033[32m[Orange] Cancelled. Returned to SSH.\033[0m\r\n")
			i.lineEditor.Reset()
			i.remoteIn.Write([]byte{'\r'})
			return

		case r == 0x7F || r == '\b': // Backspace
			i.lineEditor.Backspace()

		case r == 0x01: // Ctrl+A — Home
			i.lineEditor.Home()

		case r == 0x05: // Ctrl+E — End
			i.lineEditor.End()

		case r == 0x0B: // Ctrl+K — Kill to end
			i.lineEditor.KillToEnd()

		case r == 0x15: // Ctrl+U — Kill to start
			i.lineEditor.KillToStart()

		case r == 0x17: // Ctrl+W — Kill previous word
			i.lineEditor.KillPrevWord()

		case r >= 0x20: // Printable character
			i.lineEditor.Insert(r)
		}

		rest = rest[size:]
	}
}

// submitQuestion sends the user's question to the LLM and handles the response.
func (i *Interceptor) submitQuestion(question string) {
	fmt.Print("\r\n\033[33m[Orange] Thinking...\033[0m\r\n")

	prompt := i.buildPrompt(question)
	answer, err := llm.AskAssistant(i.cfg, prompt)
	if err != nil {
		fmt.Printf("\r\n\033[31m[Orange Error] %v\033[0m\r\n", err)
		i.assistant = false
		return
	}

	if i.cfg.Autonomous {
		i.executeAutonomousLoop(question, answer)
	} else {
		i.handleManualAnswer(answer)
	}

	i.assistant = false
}

// buildPrompt constructs the full LLM prompt with context, history, and question.
func (i *Interceptor) buildPrompt(question string) string {
	return fmt.Sprintf("%s\n\nTerminal History:\n```\n%s\n```\n\nUser Question: %s",
		i.ctx.GetContextPrompt(), i.history.String(), question)
}

// handleManualAnswer processes the LLM response in manual mode:
// displays the answer and handles command suggestion/approval.
func (i *Interceptor) handleManualAnswer(answer string) {
	// Print answer with formatting
	lines := strings.Split(answer, "\n")
	for _, line := range lines {
		fmt.Printf("\r\033[36m%s\033[0m\n", line)
	}

	// Check if AI suggested a command
	cmdToRun := extractCommand(answer)
	if cmdToRun == "" {
		i.remoteIn.Write([]byte{'\r'})
		return
	}

	if i.cfg.ApprovalMode == "never" {
		// Execute directly (risky)
		fmt.Printf("\r\n\033[35m[Orange] Executing automatically (approval-policy=never): %s\033[0m\r\n", cmdToRun)
		i.ctx.LastCommand = cmdToRun
		i.remoteIn.Write([]byte(cmdToRun + "\n"))
	} else {
		// Prompt for approval
		i.pendingCmd = cmdToRun
		i.awaitingConf = true
		fmt.Printf("\r\n\033[33m[Orange] AI suggests running this command:\033[0m \033[1;37m%s\033[0m", cmdToRun)
		fmt.Print("\r\n\033[33mDo you want to execute it? [Y/n]: \033[0m")
	}
}

// executeAutonomousLoop runs the autonomous agent loop: parse LLM response, execute commands,
// feed results back to LLM, repeat until DONE.
func (i *Interceptor) executeAutonomousLoop(question, answer string) {
	for {
		agentResp, err := agent.ParseAgentResponse(answer)
		if err != nil {
			fmt.Printf("\r\n\033[31m[Orange Error] Failed to parse JSON: %v\nRaw Output:\n%s\033[0m\r\n", err, answer)
			break
		}

		fmt.Printf("\r\n\033[36m[Agent Thought]\033[0m \r\n%s\r\n", strings.ReplaceAll(agentResp.Thought, "\n", "\r\n"))

		if agentResp.Status == "DONE" || agentResp.Action == "finish" {
			finalAns := strings.ReplaceAll(agentResp.FinalAnswer, "\n", "\r\n")
			fmt.Printf("\r\n\033[32m[Agent Finished]\033[0m \r\n%s\r\n", finalAns)
			i.remoteIn.Write([]byte{'\r'})
			break
		}

		if agentResp.Action != "exec_command" || agentResp.Command == "" {
			break
		}

		i.ctx.LastCommand = agentResp.Command
		i.executeAgentCommand(agentResp)

		// Re-prompt the LLM with updated context
		fmt.Print("\r\n\033[33m[Orange] Thinking...\033[0m\r\n")
		prompt := i.buildPrompt(question)
		answer, err = llm.AskAssistant(i.cfg, prompt)
		if err != nil {
			fmt.Printf("\r\n\033[31m[Orange Error] %v\033[0m\r\n", err)
			break
		}
	}
}

// executeAgentCommand executes a single command from the autonomous agent,
// either in foreground (interactive) or background (silent) mode.
func (i *Interceptor) executeAgentCommand(resp *agent.AgentResponse) {
	if resp.Interactive {
		fmt.Printf("\r\n\033[35m[Agent Executing in Foreground] %s\033[0m\r\n", resp.Command)
		i.recording = true
		i.recordBuf.Reset()

		// Drain channel if any old signal is left
		select {
		case <-i.recordDone:
		default:
		}

		probeCmd := fmt.Sprintf("echo \"--ORANGE_START--\" ; %s ; echo \"--ORANGE_EXIT_CODE:$?--\" ; echo \"--ORANGE_PWD:$PWD--\"\n", resp.Command)
		i.remoteIn.Write([]byte(probeCmd))

		// Block and wait for recording to finish
		<-i.recordDone
	} else {
		fmt.Printf("\r\n\033[33m[Agent Executing in Background] %s\033[0m\r\n", resp.Command)

		stdout, stderr, exitCode, err := i.sshClient.ExecuteBackground(resp.Command, i.ctx.CurrentDir)

		i.ctx.LastExitCode = fmt.Sprintf("%d", exitCode)
		out := stdout
		if stderr != "" {
			out += "\n[Stderr]:\n" + stderr
		}
		if err != nil && exitCode == -1 {
			out += fmt.Sprintf("\n[SSH Execution Error]: %v", err)
		}
		i.ctx.LastOutput = strings.TrimSpace(out)
	}
}
