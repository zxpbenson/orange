package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/zxpbenson/orange/internal/config"
	"github.com/zxpbenson/orange/internal/sshclient"
	"github.com/zxpbenson/orange/internal/tty"
	"golang.org/x/term"
)

func run() error {
	var port int
	var identityFile string
	var approvalMode string
	var autonomous bool

	fs := flag.NewFlagSet("orange", flag.ContinueOnError)
	fs.IntVar(&port, "p", 22, "Port to connect to on the remote host")
	fs.StringVar(&identityFile, "i", "", "Selects a file from which the identity (private key) for public key authentication is read")
	fs.StringVar(&approvalMode, "approval-policy", "always", "Approval policy for AI commands: always, never (risky)")
	fs.BoolVar(&autonomous, "autonomous", false, "Enable autonomous agentic loop")

	target := ""
	args := os.Args[1:]
	
	// Parse interleaved flags and positional arguments properly
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		
		if len(fs.Args()) > 0 {
			if target == "" {
				target = fs.Arg(0)
			}
			args = fs.Args()[1:]
		} else {
			break
		}
	}

	if target == "" {
		return fmt.Errorf("Usage: orange [-p port] [-i identity_file] [--approval-policy always|never] [user@]host")
	}

	var username, host string
	lastAt := strings.LastIndex(target, "@")
	if lastAt != -1 {
		username = target[:lastAt]
		host = target[lastAt+1:]
	} else {
		host = target
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("could not get current user: %v", err)
		}
		username = u.Username
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: failed to load config: %v\r\n", err)
		cfg = &config.Config{
			LLMEndpoint: "https://api.openai.com/v1",
			Model:       "gpt-4o",
		}
	}
	cfg.ApprovalMode = approvalMode
	cfg.Autonomous = autonomous

	fmt.Printf("Connecting to %s@%s:%d...\n", username, host, port)
	client, err := sshclient.Connect(username, host, port, identityFile)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	if err := client.RequestPty(session); err != nil {
		return fmt.Errorf("failed to request pty: %v", err)
	}

	// Put local terminal into raw mode ONLY after known_hosts prompt is done
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	// Setup pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to setup stdin: %v", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to setup stdout: %v", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to setup stderr: %v", err)
	}

	interceptor := tty.NewInterceptor(cfg, stdin, stdout, stderr)
	interceptor.SetClient(client)

	fmt.Printf("\r\n\033[32m[Orange] Connected. Press %s to ask the AI assistant.\033[0m\r\n", strings.ToUpper(cfg.ShortcutKey))

	// Start the remote shell FIRST
	if err := client.Shell(session); err != nil {
		return fmt.Errorf("failed to start shell: %v", err)
	}

	// Start the interceptor in a goroutine so it doesn't block session.Wait()
	go interceptor.Start()

	session.Wait()
	fmt.Print("\r\n\033[32m[Orange] Disconnected.\033[0m\r\n")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
