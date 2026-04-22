package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/zxpbenson/orange/internal/config"
)

type MCPClient struct {
	Cmd     *exec.Cmd
	Stdin   io.WriteCloser
	Scanner *bufio.Scanner
	IDCount int
}

type JSONRPCReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type JSONRPCRes struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func StartMCP(cfg *config.MCPServerConfig) (*MCPClient, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = append(cmd.Environ(), cfg.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client := &MCPClient{
		Cmd:     cmd,
		Stdin:   stdin,
		Scanner: bufio.NewScanner(stdout),
		IDCount: 1,
	}

	// Send initialize
	_, err = client.Call("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "orange-client",
			"version": "1.0",
		},
	})

	if err != nil {
		return nil, fmt.Errorf("mcp init failed: %v", err)
	}

	return client, nil
}

func (c *MCPClient) Call(method string, params interface{}) (*JSONRPCRes, error) {
	req := JSONRPCReq{
		JSONRPC: "2.0",
		ID:      c.IDCount,
		Method:  method,
		Params:  params,
	}
	c.IDCount++

	b, _ := json.Marshal(req)
	fmt.Fprintf(c.Stdin, "%s\n", string(b))

	if c.Scanner.Scan() {
		var res JSONRPCRes
		if err := json.Unmarshal(c.Scanner.Bytes(), &res); err != nil {
			return nil, err
	}
		if res.Error != nil {
			return nil, fmt.Errorf("mcp error: %v", res.Error.Message)
		}
		return &res, nil
	}
	return nil, fmt.Errorf("mcp stream closed")
}

func (c *MCPClient) Close() {
	c.Stdin.Close()
	c.Cmd.Process.Kill()
}
