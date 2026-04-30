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

type cliArgs struct {
	username     string
	host         string
	port         int
	identityFile string
	approvalMode string
	autonomous   bool
}

// parseArgs parses CLI flags and the positional [user@]host argument.
func parseArgs() (*cliArgs, error) {
	var args cliArgs

	fs := flag.NewFlagSet("orange", flag.ContinueOnError)
	fs.IntVar(&args.port, "p", 22, "Port to connect to on the remote host")
	fs.StringVar(&args.identityFile, "i", "", "Selects a file from which the identity (private key) for public key authentication is read")
	fs.StringVar(&args.approvalMode, "approval-policy", "always", "Approval policy for AI commands: always, never (risky)")
	fs.BoolVar(&args.autonomous, "autonomous", false, "Enable autonomous agentic loop")

	target := ""
	remaining := os.Args[1:]

	// Parse interleaved flags and positional arguments properly
	for len(remaining) > 0 {
		if err := fs.Parse(remaining); err != nil {
			return nil, err
		}

		if len(fs.Args()) > 0 {
			if target == "" {
				target = fs.Arg(0)
			}
			remaining = fs.Args()[1:]
		} else {
			break
		}
	}

	if target == "" {
		return nil, fmt.Errorf("Usage: orange [-p port] [-i identity_file] [--approval-policy always|never] [user@]host")
	}

	// Split user@host
	lastAt := strings.LastIndex(target, "@")
	if lastAt != -1 {
		args.username = target[:lastAt]
		args.host = target[lastAt+1:]
	} else {
		args.host = target
		u, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("could not get current user: %v", err)
		}
		args.username = u.Username
	}

	return &args, nil
}

// loadConfigWithOverrides loads config from file and merges in CLI flags.
func loadConfigWithOverrides(args *cliArgs) *config.Config {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Warning: failed to load config: %v\r\n", err)
		cfg = &config.Config{
			LLMEndpoint: "https://api.openai.com/v1",
			Model:       "gpt-4o",
		}
	}
	cfg.ApprovalMode = args.approvalMode
	cfg.Autonomous = args.autonomous
	return cfg
}

func run() error {
	args, err := parseArgs()
	if err != nil {
		return err
	}

	cfg := loadConfigWithOverrides(args)

	// Establish SSH connection
	fmt.Printf("Connecting to %s@%s:%d...\n", args.username, args.host, args.port)
	client, err := sshclient.Connect(args.username, args.host, args.port, args.identityFile)
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

	// Launch interceptor and remote shell
	interceptor := tty.NewInterceptor(cfg, stdin, stdout, stderr)
	interceptor.SetClient(client)

	fmt.Printf("\r\n\033[32m[Orange] Connected. Press %s to ask the AI assistant.\033[0m\r\n", strings.ToUpper(cfg.ShortcutKey))

	if err := client.Shell(session); err != nil {
		return fmt.Errorf("failed to start shell: %v", err)
	}

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
