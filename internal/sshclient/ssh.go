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
	var auths []ssh.AuthMethod
	var agentConn net.Conn

	// 1. Try SSH Agent first
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentConn = sshAgent // Keep this connection alive until we are done authenticating
		auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
	}

	// 2. Try Identity File if provided, otherwise fallback to default ~/.ssh/id_rsa
	if identityFile == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			identityFile = filepath.Join(home, ".ssh", "id_rsa")
		}
	} else if strings.HasPrefix(identityFile, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			identityFile = filepath.Join(home, identityFile[2:])
		}
	}

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
		fmt.Println() // Print a newline after password input
		if err != nil {
			return "", err
		}
		return string(passwordBytes), nil
	}))

	// 4. Setup HostKeyCallback logic to mimic standard SSH known_hosts behavior
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	knownHostsFile := filepath.Join(home, ".ssh", "known_hosts")
	// Ensure the file exists so we can append to it later if needed
	file, err := os.OpenFile(knownHostsFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open or create known_hosts file: %v", err)
	}
	file.Close()

	hkCallback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse known_hosts file: %v", err)
	}

	hostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := hkCallback(hostname, remote, key)
		if err == nil {
			return nil // Key is known and matches
		}

		// Use Go's type assertion for knownhosts.KeyError
		// If len(keyErr.Want) == 0, it means the key is unknown (first time connection)
		// If len(keyErr.Want) > 0, it means the key mismatched (MITM attack or host key changed)
		if keyErr, ok := err.(*knownhosts.KeyError); ok {
			if len(keyErr.Want) > 0 {
				return fmt.Errorf("\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\nIT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\n%v", err)
			}
			
			// Key is unknown. We should prompt the user on the terminal before connecting.
			fingerprint := ssh.FingerprintSHA256(key)
			fmt.Printf("The authenticity of host '%s (%s)' can't be established.\n", host, remote.String())
			fmt.Printf("%s key fingerprint is %s.\n", key.Type(), fingerprint)
			fmt.Print("Are you sure you want to continue connecting (yes/no/[fingerprint])? ")
			
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
				if answer == "yes" || answer == "y" {
					// Append to known_hosts
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

		// Some other error
		return err
	}

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
