package tty

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/zxpbenson/orange/internal/config"
	"github.com/zxpbenson/orange/internal/llm"
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
	remoteIn     io.Writer
	remoteOut    io.Reader
	remoteErr    io.Reader
	history      *RingBuffer
	assistant    bool
	assistBuf    bytes.Buffer
	pendingCmd   string
	awaitingConf bool
}

func NewInterceptor(cfg *config.Config, remoteIn io.Writer, remoteOut, remoteErr io.Reader) *Interceptor {
	return &Interceptor{
		cfg:       cfg,
		remoteIn:  remoteIn,
		remoteOut: remoteOut,
		remoteErr: remoteErr,
		history:   NewRingBuffer(8192), // Keep last 8KB of output
	}
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

func (i *Interceptor) Start() {
	// Read from remote and write to local stdout + history buffer
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := i.remoteOut.Read(buf)
			if n > 0 {
				os.Stdout.Write(buf[:n])
				i.history.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
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
	}()

	// Read from local stdin and write to remote or intercept
	// To support multi-byte utf8 (e.g. Chinese) input in raw mode,
	// we need a buffer that handles incomplete runes.
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}

		inputData := buf[:n]

		// If we are waiting for a Y/n confirmation
		if i.awaitingConf {
			char := inputData[0]
			if char == 'y' || char == 'Y' || char == '\r' || char == '\n' {
				fmt.Print("\r\n\033[32m[Orange] Executing...\033[0m\r\n")
				i.remoteIn.Write([]byte(i.pendingCmd + "\n"))
			} else {
				fmt.Print("\r\n\033[31m[Orange] Cancelled.\033[0m\r\n")
			}
			i.awaitingConf = false
			continue
		}

		// Handle toggling assistant mode
		// Some terminals might send Ctrl+A within a larger block, but usually it's solitary
		shortcutByte := parseShortcutKey(i.cfg.ShortcutKey)
		for j := 0; j < len(inputData); j++ {
			char := inputData[j]
			if char == shortcutByte { // Dynamic shortcut
				i.assistant = !i.assistant
				if i.assistant {
					fmt.Print("\r\n\033[33m[Orange Assistant] Enter your question (press Enter to submit, Ctrl+C to cancel): \033[0m")
					i.assistBuf.Reset()
				} else {
					fmt.Print("\r\n\033[32m[Orange] Returned to SSH.\033[0m\r\n")
				}
				// Remove Ctrl+A from inputData so we don't process it further
				inputData = append(inputData[:j], inputData[j+1:]...)
				j-- // Adjust index
			}
		}

		if len(inputData) == 0 {
			continue
		}

		if i.assistant {
			// We process the inputData rune by rune to properly handle UTF-8 backspaces and prints
			rest := inputData
			for len(rest) > 0 {
				r, size := utf8.DecodeRune(rest)
				if r == utf8.RuneError && size == 1 {
					// Not enough bytes to decode, or invalid. Just write it as raw byte for now.
					i.assistBuf.WriteByte(rest[0])
					fmt.Print(string(rest[0]))
					rest = rest[1:]
					continue
				}

				// Handle special characters
				if r == '\r' || r == '\n' {
					fmt.Print("\r\n\033[33m[Orange] Thinking...\033[0m\r\n")
					question := i.assistBuf.String()
					prompt := fmt.Sprintf("Terminal History:\n```\n%s\n```\n\nUser Question: %s", i.history.String(), question)
					
					answer, err := llm.AskAssistant(i.cfg, prompt)
					if err != nil {
						fmt.Printf("\r\n\033[31m[Orange Error] %v\033[0m\r\n", err)
						i.assistant = false
						rest = nil
						break
					}

					// Print answer with basic formatting
					lines := strings.Split(answer, "\n")
					for _, line := range lines {
						fmt.Printf("\r\033[36m%s\033[0m\n", line)
					}

					// Check if AI suggested a command
					cmdToRun := extractCommand(answer)
					if cmdToRun != "" {
						if i.cfg.ApprovalMode == "never" {
							// Execute directly (risky)
							fmt.Printf("\r\n\033[35m[Orange] Executing automatically (approval-policy=never): %s\033[0m\r\n", cmdToRun)
							i.remoteIn.Write([]byte(cmdToRun + "\n"))
						} else {
							// Prompt for approval
							i.pendingCmd = cmdToRun
							i.awaitingConf = true
							fmt.Printf("\r\n\033[33m[Orange] AI suggests running this command:\033[0m \033[1;37m%s\033[0m", cmdToRun)
							fmt.Print("\r\n\033[33mDo you want to execute it? [Y/n]: \033[0m")
						}
					}
					
					// Exit assistant mode after answering
					i.assistant = false
					rest = nil
					break
				} else if r == 0x03 { // Ctrl+C
					i.assistant = false
					fmt.Print("\r\n\033[32m[Orange] Cancelled. Returned to SSH.\033[0m\r\n")
					rest = nil
					break
				} else if r == 0x7F || r == '\b' { // Backspace
					// We need to pop the last rune from assistBuf, not just the last byte
					bufBytes := i.assistBuf.Bytes()
					if len(bufBytes) > 0 {
						_, lastRuneSize := utf8.DecodeLastRune(bufBytes)
						i.assistBuf.Truncate(len(bufBytes) - lastRuneSize)
						
						// Erase visually. Note: multi-byte char might take up more terminal columns (e.g. Chinese is 2 wide)
						// A simple backspace+space+backspace works for most 1-wide chars, but for wide chars 
						// we'll issue two backspaces if it was a multi-byte sequence to be safe.
						if lastRuneSize > 1 {
							fmt.Print("\b\b  \b\b")
						} else {
							fmt.Print("\b \b")
						}
					}
				} else {
					// Normal character
					i.assistBuf.Write(rest[:size])
					fmt.Print(string(rest[:size]))
				}

				rest = rest[size:]
			}
		} else {
			// Normal SSH passthrough: write exactly what we read
			i.remoteIn.Write(inputData)
		}
	}
}
