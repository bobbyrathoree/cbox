// Package tunnel provides SSH reverse tunnel functionality.
package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// PortMapping defines a port forwarding rule
type PortMapping struct {
	LocalPort  int
	RemotePort int
}

// Config holds tunnel configuration
type Config struct {
	Host     string
	User     string
	Port     int
	Mappings []PortMapping
	Insecure bool // Skip host key verification (dangerous!)
}

// Tunnel manages SSH reverse tunnel connections
type Tunnel struct {
	config    *ssh.ClientConfig
	host      string
	port      int
	client    *ssh.Client
	listeners []net.Listener
	mu        sync.Mutex
	console   Logger
}

// Logger interface for tunnel output
type Logger interface {
	Info(format string, args ...interface{})
	Success(format string, args ...interface{})
	Error(format string, args ...interface{})
	Warn(format string, args ...interface{})
}

// ParseHost parses user@host:port format
func ParseHost(hostStr string) (username, host string, port int) {
	port = 22 // Default SSH port

	// Check for port
	if colonIdx := strings.LastIndex(hostStr, ":"); colonIdx != -1 {
		// Make sure it's not part of IPv6
		if !strings.Contains(hostStr[colonIdx:], "]") {
			fmt.Sscanf(hostStr[colonIdx+1:], "%d", &port)
			hostStr = hostStr[:colonIdx]
		}
	}

	// Check for user@
	if atIdx := strings.Index(hostStr, "@"); atIdx != -1 {
		username = hostStr[:atIdx]
		host = hostStr[atIdx+1:]
	} else {
		// Default to current user
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
		host = hostStr
	}

	return
}

// New creates a new SSH tunnel
func New(cfg Config, logger Logger) (*Tunnel, error) {
	// Get SSH auth methods
	authMethods, err := getAuthMethods()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth methods: %w", err)
	}

	// Get host key callback
	hostKeyCallback, err := getHostKeyCallback()
	if err != nil {
		if cfg.Insecure {
			hostKeyCallback = ssh.InsecureIgnoreHostKey()
			if logger != nil {
				logger.Warn("Using insecure mode: accepting any host key")
			}
		} else {
			return nil, fmt.Errorf("no known_hosts file found at ~/.ssh/known_hosts: %w\n"+
				"  Fix: run 'ssh-keyscan %s >> ~/.ssh/known_hosts'\n"+
				"  Or:  use --insecure to skip host key verification (not recommended)",
				err, cfg.Host)
		}
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}

	return &Tunnel{
		config:  sshConfig,
		host:    cfg.Host,
		port:    cfg.Port,
		console: logger,
	}, nil
}

// Connect establishes the SSH connection
func (t *Tunnel) Connect(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", t.host, t.port)
	t.log("Connecting to %s...", addr)

	client, err := ssh.Dial("tcp", addr, t.config)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	t.client = client
	t.success("Connected to %s", t.host)
	return nil
}

// Forward creates a reverse tunnel for a port mapping
func (t *Tunnel) Forward(ctx context.Context, mapping PortMapping) error {
	// Remote listens, forwards to local
	remoteAddr := fmt.Sprintf("0.0.0.0:%d", mapping.RemotePort)

	listener, err := t.client.Listen("tcp", remoteAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on remote %d: %w", mapping.RemotePort, err)
	}

	t.mu.Lock()
	t.listeners = append(t.listeners, listener)
	t.mu.Unlock()

	t.success("Forwarding :%d -> localhost:%d", mapping.RemotePort, mapping.LocalPort)

	// Accept connections
	go func() {
		for {
			remote, err := listener.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				t.err("Accept error: %s", err)
				continue
			}
			go t.handleConnection(remote, mapping.LocalPort)
		}
	}()

	return nil
}

// handleConnection proxies between remote and local
func (t *Tunnel) handleConnection(remote net.Conn, localPort int) {
	defer remote.Close()

	local, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		t.err("Failed to connect to localhost:%d: %s", localPort, err)
		return
	}
	defer local.Close()

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(local, remote)
	}()

	go func() {
		defer wg.Done()
		io.Copy(remote, local)
	}()

	wg.Wait()
}

// Close closes all connections
func (t *Tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, l := range t.listeners {
		l.Close()
	}

	if t.client != nil {
		return t.client.Close()
	}

	return nil
}

// getAuthMethods returns available SSH auth methods
func getAuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try SSH agent first
	if agentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentClient := agent.NewClient(agentConn)
		methods = append(methods, ssh.PublicKeysCallback(agentClient.Signers))
	}

	// Try default key files
	home, _ := os.UserHomeDir()
	keyFiles := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}

	for _, keyFile := range keyFiles {
		if key, err := loadPrivateKey(keyFile); err == nil {
			methods = append(methods, ssh.PublicKeys(key))
		}
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no SSH auth methods available")
	}

	return methods, nil
}

// loadPrivateKey loads a private key from file
func loadPrivateKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return ssh.ParsePrivateKey(data)
}

// getHostKeyCallback returns a host key callback using known_hosts
func getHostKeyCallback() (ssh.HostKeyCallback, error) {
	home, _ := os.UserHomeDir()
	knownHostsFile := filepath.Join(home, ".ssh", "known_hosts")

	return knownhosts.New(knownHostsFile)
}

// Logging helpers
func (t *Tunnel) log(format string, args ...interface{}) {
	if t.console != nil {
		t.console.Info(format, args...)
	}
}

func (t *Tunnel) success(format string, args ...interface{}) {
	if t.console != nil {
		t.console.Success(format, args...)
	}
}

func (t *Tunnel) err(format string, args ...interface{}) {
	if t.console != nil {
		t.console.Error(format, args...)
	}
}
