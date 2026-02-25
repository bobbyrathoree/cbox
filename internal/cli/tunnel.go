package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	tunnelHost     string
	tunnelPorts    []string
	tunnelInsecure bool
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Create SSH reverse tunnel to expose local services",
	Long: `Create an SSH reverse tunnel to expose local services to a remote server.

This allows you to share your local development environment with others
by making your services accessible via the remote server.

Requirements:
  - SSH access to the remote server
  - SSH key authentication (password auth not supported)

Port format: local:remote (e.g., 3000:8080)
  - local: The port your service is running on locally
  - remote: The port to expose on the remote server

Examples:
  cbox tunnel --host user@server.com --port 3000:8080
  cbox tunnel --host server.com -p 3000:8080 -p 5432:5432
  cbox tunnel --host user@server.com:2222 -p 3000:80`,
	RunE: runTunnel,
}

func init() {
	tunnelCmd.Flags().StringVar(&tunnelHost, "host", "", "remote SSH host (user@host:port)")
	tunnelCmd.Flags().StringArrayVarP(&tunnelPorts, "port", "p", nil, "port mapping (local:remote)")

	tunnelCmd.Flags().BoolVar(&tunnelInsecure, "insecure", false, "skip host key verification (dangerous!)")

	tunnelCmd.MarkFlagRequired("host")
}

func runTunnel(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	console := output.NewWithOptions(verbose, quiet)

	// Parse host
	user, host, port := tunnel.ParseHost(tunnelHost)
	if host == "" {
		return fmt.Errorf("invalid host: %s", tunnelHost)
	}

	// Get port mappings
	var mappings []tunnel.PortMapping

	// If no ports specified, try to get from config
	if len(tunnelPorts) == 0 {
		cfg, err := loadConfig()
		if err == nil {
			// Use service ports from config
			for _, svc := range cfg.Services {
				if svc.Port > 0 {
					mappings = append(mappings, tunnel.PortMapping{
						LocalPort:  svc.Port,
						RemotePort: svc.Port,
					})
				}
			}
		}
	} else {
		// Parse port flags
		for _, p := range tunnelPorts {
			mapping, err := parsePortMapping(p)
			if err != nil {
				return err
			}
			mappings = append(mappings, mapping)
		}
	}

	if len(mappings) == 0 {
		return fmt.Errorf("no port mappings specified - use --port or configure services in cbox.yaml")
	}

	console.Header("SSH Tunnel")
	console.Info("Host: %s@%s:%d", user, host, port)
	console.Newline()

	// Create tunnel
	tun, err := tunnel.New(tunnel.Config{
		Host:     host,
		User:     user,
		Port:     port,
		Mappings: mappings,
		Insecure: tunnelInsecure,
	}, console)
	if err != nil {
		return err
	}
	defer tun.Close()

	// Connect
	if err := tun.Connect(ctx); err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to connect: %s", err),
			"Check your SSH credentials and that the host is reachable",
		)
		return err
	}

	// Set up port forwarding
	for _, m := range mappings {
		if err := tun.Forward(ctx, m); err != nil {
			console.Error("Failed to forward port %d: %s", m.LocalPort, err)
			continue
		}
	}

	console.Newline()
	console.Info("Tunnel active! Your services are available at:")
	for _, m := range mappings {
		console.Success("  localhost:%d -> %s:%d", m.LocalPort, host, m.RemotePort)
	}
	console.Newline()
	console.Dim("Press Ctrl+C to stop")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	console.Newline()
	console.Info("Closing tunnel...")

	return nil
}

// parsePortMapping parses "local:remote" format
func parsePortMapping(s string) (tunnel.PortMapping, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return tunnel.PortMapping{}, fmt.Errorf("invalid port mapping: %s (expected local:remote)", s)
	}

	local, err := strconv.Atoi(parts[0])
	if err != nil {
		return tunnel.PortMapping{}, fmt.Errorf("invalid local port: %s", parts[0])
	}

	remote, err := strconv.Atoi(parts[1])
	if err != nil {
		return tunnel.PortMapping{}, fmt.Errorf("invalid remote port: %s", parts[1])
	}

	return tunnel.PortMapping{
		LocalPort:  local,
		RemotePort: remote,
	}, nil
}
