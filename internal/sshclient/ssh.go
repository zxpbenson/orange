package sshclient

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

type Client struct {
	client    *ssh.Client
	session   *ssh.Session
	agentConn net.Conn
}

func Connect(user, host string, port int, identityFile string) (*Client, error) {
	// 1. Build authentication methods
	auths, agentConn := buildAuthMethods(user, host, identityFile)

	// 2. Build host key verification callback
	hostKeyCallback, err := buildHostKeyCallback(host)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}

	// 3. Dial the remote server
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}

	return &Client{client: client, agentConn: agentConn}, nil
}

// resolveIdentityFile resolves the identity file path, expanding ~ and falling back to ~/.ssh/id_rsa.
func resolveIdentityFile(identityFile string) string {
	if identityFile == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, ".ssh", "id_rsa")
		}
		return ""
	}

	if strings.HasPrefix(identityFile, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, identityFile[2:])
		}
	}

	return identityFile
}

// buildAuthMethods constructs the SSH authentication method list in priority order:
// SSH Agent -> Identity File -> Interactive Password.
func buildAuthMethods(user, host string, identityFile string) ([]ssh.AuthMethod, net.Conn) {
	var auths []ssh.AuthMethod
	var agentConn net.Conn

	// 1. Try SSH Agent first
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentConn = sshAgent
		auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
	}

	// 2. Try identity file
	identityFile = resolveIdentityFile(identityFile)
	if identityFile != "" {
		key, err := os.ReadFile(identityFile)
		if err == nil {
			signer, err := ssh.ParsePrivateKey(key)
			if err == nil {
				auths = append(auths, ssh.PublicKeys(signer))
			}
		}
	}

	// 3. Add interactive password fallback
	auths = append(auths, ssh.PasswordCallback(func() (string, error) {
		fmt.Printf("%s@%s's password: ", user, host)
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", err
		}
		return string(passwordBytes), nil
	}))

	return auths, agentConn
}

// buildHostKeyCallback creates a host key verification callback that mimics standard SSH known_hosts behavior:
// - Returns nil if the key is already known and matches.
// - Prompts the user on first connection and appends the key to known_hosts.
// - Returns an error with a warning if the key has changed (potential MITM).
func buildHostKeyCallback(host string) (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	knownHostsFile := filepath.Join(home, ".ssh", "known_hosts")
	// Ensure the file exists so we can append to it later if needed
	f, err := os.OpenFile(knownHostsFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open or create known_hosts file: %v", err)
	}
	f.Close()

	hkCallback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse known_hosts file: %v", err)
	}

	callback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := hkCallback(hostname, remote, key)
		if err == nil {
			return nil // Key is known and matches
		}

		keyErr, ok := err.(*knownhosts.KeyError)
		if !ok {
			return err
		}

		// Key mismatched — possible MITM attack
		if len(keyErr.Want) > 0 {
			return fmt.Errorf(
				"\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n"+
					"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n"+
					"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n"+
					"IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\n%v", err)
		}

		// Key is unknown — first time connection, prompt the user
		return promptAndAddHostKey(host, hostname, remote, key, knownHostsFile)
	}

	return callback, nil
}

// promptAndAddHostKey prompts the user to verify a new host key and appends it to known_hosts on acceptance.
func promptAndAddHostKey(host, hostname string, remote net.Addr, key ssh.PublicKey, knownHostsFile string) error {
	fingerprint := ssh.FingerprintSHA256(key)
	fmt.Printf("The authenticity of host '%s (%s)' can't be established.\n", host, remote.String())
	fmt.Printf("%s key fingerprint is %s.\n", key.Type(), fingerprint)
	fmt.Print("Are you sure you want to continue connecting (yes/no/[fingerprint])? ")

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer == "yes" || answer == "y" {
			f, err := os.OpenFile(knownHostsFile, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return fmt.Errorf("failed to open known_hosts to append: %v", err)
			}
			defer f.Close()

			line := knownhosts.Line([]string{hostname}, key)
			if _, err := f.WriteString(line + "\n"); err != nil {
				return fmt.Errorf("failed to write to known_hosts: %v", err)
			}
			fmt.Printf("Warning: Permanently added '%s' (%s) to the list of known hosts.\n", host, key.Type())
			return nil
		}
	}

	return fmt.Errorf("Host key verification failed.")
}

func (c *Client) NewSession() (*ssh.Session, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, err
	}
	c.session = session
	return session, nil
}

func (c *Client) RequestPty(session *ssh.Session) error {
	fd := int(os.Stdin.Fd())
	termWidth, termHeight, err := term.GetSize(fd)
	if err != nil {
		termWidth, termHeight = 80, 24
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	termEnv := os.Getenv("TERM")
	if termEnv == "" {
		termEnv = "xterm-256color"
	}
	return session.RequestPty(termEnv, termHeight, termWidth, modes)
}

func (c *Client) Shell(session *ssh.Session) error {
	return session.Shell()
}

func (c *Client) Close() {
	if c.session != nil {
		c.session.Close()
	}
	if c.client != nil {
		c.client.Close()
	}
	if c.agentConn != nil {
		c.agentConn.Close()
	}
}

// ExecuteBackground runs a command in a new, independent SSH session without a PTY.
// It optionally takes a workDir to execute the command within that directory context.
func (c *Client) ExecuteBackground(cmd string, workDir string) (stdout string, stderr string, exitCode int, err error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", "", -1, err
	}
	defer session.Close()

	if workDir != "" {
		// Make sure we cd to the correct directory first
		cmd = fmt.Sprintf("cd '%s' && %s", workDir, cmd)
	}

	// Provide a bit of a wrapper to ensure we always run via bash (if available) to support pipes/redirects
	cmd = fmt.Sprintf("bash -c '%s'", strings.ReplaceAll(cmd, "'", "'\\''"))

	var stdoutBuf, stderrBuf strings.Builder
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	err = session.Run(cmd)
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			exitCode = -1
		}
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}
