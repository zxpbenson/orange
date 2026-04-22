package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	ID      *int            `json:"id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// We will mock a very simple MCP server that implements one tool: "get_time"
func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		var req JSONRPCMessage
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		if req.Method == "initialize" {
			resBody := `{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"orange-mock-mcp","version":"1.0.0"}}`
			res := JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(resBody),
			}
			send(res)
		} else if req.Method == "tools/list" {
			resBody := `{"tools":[{"name":"get_server_time","description":"Returns the current time on the server."}]}`
			res := JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(resBody),
			}
			send(res)
		} else if req.Method == "tools/call" {
			resBody := `{"content":[{"type":"text","text":"The current time is 2025-01-01 12:00:00 UTC"}]}`
			res := JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(resBody),
			}
			send(res)
		} else {
			// Ignore others for this mock
			res := JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32601, Message: "Method not found"},
			}
			send(res)
		}
	}
}

func send(msg JSONRPCMessage) {
	b, _ := json.Marshal(msg)
	// Ensure single line output
	fmt.Println(strings.ReplaceAll(string(b), "\n", ""))
}
