// Package runtime handles container lifecycle management.
package runtime

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// Docker wraps Docker CLI commands for container management.
// We use CLI instead of SDK for v0.1 for simplicity.
type Docker struct {
	console *output.Console
}

// New creates a new Docker runtime.
func New(console *output.Console) *Docker {
	return &Docker{
		console: console,
	}
}

// Container represents a running container.
type Container struct {
	ID      string
	Name    string
	Image   string
	Status  string
	Ports   []string
	Health  string
	Network string
}

// ContainerConfig contains options for creating a container.
type ContainerConfig struct {
	Name           string
	Image          string
	Ports          []PortMapping
	Env            map[string]string
	Volumes        []VolumeMount
	Network        string
	NetworkAliases []string
	Command        []string
	Labels         map[string]string
	BindMounts     []BindMount // For dev mode
	Healthcheck    *HealthcheckConfig
}

// PortMapping represents a port mapping.
type PortMapping struct {
	HostPort      int
	ContainerPort int
	Protocol      string // tcp or udp
}

// VolumeMount represents a named volume mount.
type VolumeMount struct {
	Name       string
	MountPath  string
	ReadOnly   bool
}

// BindMount represents a bind mount (for dev mode).
type BindMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// HealthcheckConfig for container health checking.
type HealthcheckConfig struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

// CreateNetwork creates a Docker network.
func (d *Docker) CreateNetwork(ctx context.Context, name string) error {
	// Check if network already exists
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", name)
	if err := cmd.Run(); err == nil {
		d.console.Debug("Network %s already exists", name)
		return nil
	}

	d.console.Debug("Creating network: %s", name)
	cmd = exec.CommandContext(ctx, "docker", "network", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create network: %s", string(output))
	}

	return nil
}

// RemoveNetwork removes a Docker network.
func (d *Docker) RemoveNetwork(ctx context.Context, name string) error {
	d.console.Debug("Removing network: %s", name)
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", name)
	cmd.Run() // Ignore errors - network might not exist
	return nil
}

// CreateContainer creates a new container.
func (d *Docker) CreateContainer(ctx context.Context, cfg ContainerConfig) (string, error) {
	args := []string{"create", "--name", cfg.Name}

	// Network
	if cfg.Network != "" {
		args = append(args, "--network", cfg.Network)
	}

	// Network aliases (for service discovery)
	for _, alias := range cfg.NetworkAliases {
		args = append(args, "--network-alias", alias)
	}

	// Ports
	for _, p := range cfg.Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d", p.HostPort, p.ContainerPort))
	}

	// Environment variables
	for k, v := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Named volumes
	for _, v := range cfg.Volumes {
		mount := fmt.Sprintf("%s:%s", v.Name, v.MountPath)
		if v.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	// Bind mounts (for dev mode)
	for _, b := range cfg.BindMounts {
		mount := fmt.Sprintf("%s:%s", b.HostPath, b.ContainerPath)
		if b.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	// Labels
	for k, v := range cfg.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}

	// Healthcheck
	if cfg.Healthcheck != nil && len(cfg.Healthcheck.Test) > 0 {
		args = append(args, "--health-cmd", strings.Join(cfg.Healthcheck.Test, " "))
		args = append(args, "--health-interval", cfg.Healthcheck.Interval.String())
		args = append(args, "--health-timeout", cfg.Healthcheck.Timeout.String())
		args = append(args, "--health-retries", strconv.Itoa(cfg.Healthcheck.Retries))
		args = append(args, "--health-start-period", cfg.Healthcheck.StartPeriod.String())
	}

	// Image
	args = append(args, cfg.Image)

	// Command
	args = append(args, cfg.Command...)

	d.console.Debug("Creating container: docker %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create container: %s", string(output))
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// StartContainer starts a container.
func (d *Docker) StartContainer(ctx context.Context, nameOrID string) error {
	d.console.Debug("Starting container: %s", nameOrID)
	cmd := exec.CommandContext(ctx, "docker", "start", nameOrID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start container: %s", string(output))
	}
	return nil
}

// StopContainer stops a container.
func (d *Docker) StopContainer(ctx context.Context, nameOrID string, timeout time.Duration) error {
	d.console.Debug("Stopping container: %s", nameOrID)
	args := []string{"stop", "-t", strconv.Itoa(int(timeout.Seconds())), nameOrID}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Run() // Ignore errors - container might not be running
	return nil
}

// RestartContainer restarts a container.
func (d *Docker) RestartContainer(ctx context.Context, nameOrID string, timeout time.Duration) error {
	d.console.Debug("Restarting container: %s", nameOrID)
	args := []string{"restart", "-t", strconv.Itoa(int(timeout.Seconds())), nameOrID}
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart container: %s", string(output))
	}
	return nil
}

// RemoveContainer removes a container.
func (d *Docker) RemoveContainer(ctx context.Context, nameOrID string) error {
	d.console.Debug("Removing container: %s", nameOrID)
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", nameOrID)
	cmd.Run() // Ignore errors - container might not exist
	return nil
}

// ContainerLogs streams logs from a container.
func (d *Docker) ContainerLogs(ctx context.Context, nameOrID string, follow bool, tail int) (io.ReadCloser, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail > 0 {
		args = append(args, "--tail", strconv.Itoa(tail))
	}
	args = append(args, nameOrID)

	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Combine stdout and stderr
	return &combinedReadCloser{
		stdout: stdout,
		stderr: stderr,
		cmd:    cmd,
	}, nil
}

type combinedReadCloser struct {
	stdout io.ReadCloser
	stderr io.ReadCloser
	cmd    *exec.Cmd
}

func (c *combinedReadCloser) Read(p []byte) (int, error) {
	// Read from stdout first, then stderr
	n, err := c.stdout.Read(p)
	if err == io.EOF {
		return c.stderr.Read(p)
	}
	return n, err
}

func (c *combinedReadCloser) Close() error {
	c.stdout.Close()
	c.stderr.Close()
	return c.cmd.Wait()
}

// ContainerExec executes a command in a running container.
func (d *Docker) ContainerExec(ctx context.Context, nameOrID string, cmd []string, interactive, tty bool) error {
	args := []string{"exec"}
	if interactive {
		args = append(args, "-i")
	}
	if tty {
		args = append(args, "-t")
	}
	args = append(args, nameOrID)
	args = append(args, cmd...)

	execCmd := exec.CommandContext(ctx, "docker", args...)
	execCmd.Stdin = nil // TODO: Connect to terminal if interactive
	execCmd.Stdout = nil
	execCmd.Stderr = nil

	return execCmd.Run()
}

// ContainerExecWithOutput executes a command in a container and returns output.
// This is used for lifecycle hooks.
func (d *Docker) ContainerExecWithOutput(ctx context.Context, nameOrID string, command string) (string, error) {
	d.console.Debug("Executing in %s: %s", nameOrID, command)

	args := []string{"exec", nameOrID, "sh", "-c", command}
	execCmd := exec.CommandContext(ctx, "docker", args...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("exec failed: %w\nOutput: %s", err, string(output))
	}
	return string(output), nil
}

// ListContainers lists containers with the given labels.
func (d *Docker) ListContainers(ctx context.Context, labels map[string]string, all bool) ([]Container, error) {
	args := []string{"ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"}
	if all {
		args = append(args, "-a")
	}
	for k, v := range labels {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		// docker ps returns exit code 1 when filter matches nothing, which is not an error
		if exitErr, ok := err.(*exec.ExitError); ok {
			// If stderr is empty or just whitespace, treat as "no results"
			stderr := string(exitErr.Stderr)
			if strings.TrimSpace(stderr) == "" {
				return []Container{}, nil
			}
			// Real error - return it with stderr context
			return nil, fmt.Errorf("docker ps failed: %s", stderr)
		}
		return nil, err
	}

	var containers []Container
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			c := Container{
				ID:     parts[0],
				Name:   parts[1],
				Image:  parts[2],
				Status: parts[3],
			}
			if len(parts) >= 5 {
				c.Ports = strings.Split(parts[4], ", ")
			}
			containers = append(containers, c)
		}
	}

	return containers, nil
}

// WaitHealthy waits for a container to become healthy.
func (d *Docker) WaitHealthy(ctx context.Context, nameOrID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check container status
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Health.Status}}", nameOrID)
		output, err := cmd.Output()
		if err != nil {
			// Container might not have healthcheck
			// Check if it's running at least
			cmd = exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}}", nameOrID)
			output, err = cmd.Output()
			if err != nil {
				return fmt.Errorf("container not found: %s", nameOrID)
			}
			status := strings.TrimSpace(string(output))
			if status == "running" {
				return nil // No healthcheck, but running
			}
			if status == "exited" {
				return fmt.Errorf("container exited")
			}
		} else {
			status := strings.TrimSpace(string(output))
			if status == "healthy" {
				return nil
			}
			if status == "unhealthy" {
				return fmt.Errorf("container unhealthy")
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	return fmt.Errorf("timeout waiting for container to become healthy")
}

// CreateVolume creates a named volume.
func (d *Docker) CreateVolume(ctx context.Context, name string) error {
	// Check if volume already exists
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", name)
	if err := cmd.Run(); err == nil {
		d.console.Debug("Volume %s already exists", name)
		return nil
	}

	d.console.Debug("Creating volume: %s", name)
	cmd = exec.CommandContext(ctx, "docker", "volume", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create volume: %s", string(output))
	}

	return nil
}

// RemoveVolume removes a named volume.
func (d *Docker) RemoveVolume(ctx context.Context, name string) error {
	d.console.Debug("Removing volume: %s", name)
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", name)
	cmd.Run() // Ignore errors - volume might not exist
	return nil
}

// CheckPortAvailable checks if a port is available.
func CheckPortAvailable(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("port %d is already in use", port)
	}
	ln.Close()
	return nil
}

// FindProcessOnPort returns the PID using a port (macOS/Linux).
func FindProcessOnPort(port int) (int, error) {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-t")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	pid := strings.TrimSpace(string(output))
	if pid == "" {
		return 0, fmt.Errorf("no process found")
	}

	// Parse first PID (might be multiple)
	lines := strings.Split(pid, "\n")
	if len(lines) > 0 {
		p, err := strconv.Atoi(lines[0])
		if err == nil {
			return p, nil
		}
	}

	return 0, fmt.Errorf("could not parse PID")
}

// FindAvailablePort finds an available port starting from the preferred port.
// It tries maxAttempts ports in sequence (preferred, preferred+1, etc.)
func FindAvailablePort(preferred int, maxAttempts int) (int, error) {
	for i := 0; i < maxAttempts; i++ {
		port := preferred + i
		if err := CheckPortAvailable(port); err == nil {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port in range %d-%d", preferred, preferred+maxAttempts-1)
}

// ImageExists checks if an image exists locally.
func (d *Docker) ImageExists(ctx context.Context, image string) bool {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	return cmd.Run() == nil
}

// PullImage pulls an image from a registry.
func (d *Docker) PullImage(ctx context.Context, image string) error {
	d.console.Debug("Pulling image: %s", image)
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to pull image: %s", string(output))
	}
	return nil
}

// ContainerConfigFromService creates a ContainerConfig from a service definition.
func ContainerConfigFromService(
	name string,
	svc config.Service,
	projectName string,
	network string,
	imageName string,
	devMode bool,
) ContainerConfig {
	containerName := fmt.Sprintf("%s_%s", projectName, name)

	cfg := ContainerConfig{
		Name:           containerName,
		Image:          imageName,
		Network:        network,
		NetworkAliases: []string{name},
		Env:            svc.Env,
		Command:        svc.Command,
		Labels: map[string]string{
			"cbox.project": projectName,
			"cbox.service": name,
		},
	}

	// Ports
	if svc.Port > 0 {
		// Use HostPort if set (port was remapped due to conflict), otherwise use Port
		hostPort := svc.Port
		if svc.HostPort > 0 {
			hostPort = svc.HostPort
		}
		cfg.Ports = append(cfg.Ports, PortMapping{
			HostPort:      hostPort,
			ContainerPort: svc.Port,
		})
	}
	for _, p := range svc.Expose {
		if p != svc.Port {
			cfg.Ports = append(cfg.Ports, PortMapping{
				HostPort:      p,
				ContainerPort: p,
			})
		}
	}

	// Dev mode: use dev command and bind mounts
	if devMode {
		if len(svc.Dev.Command) > 0 {
			cfg.Command = svc.Dev.Command
		}
		// Add bind mount for source (if path is specified)
		if svc.Path != "" && svc.Dev.Sync {
			cfg.BindMounts = append(cfg.BindMounts, BindMount{
				HostPath:      svc.Path,
				ContainerPath: "/app",
			})
		}
	}

	// Healthcheck
	if svc.HasHealthcheck() {
		if svc.Healthcheck.Path != "" {
			port := svc.Port
			if port == 0 {
				port = 80
			}

			// Set sensible defaults for health check timing if not specified
			interval := svc.Healthcheck.Interval
			if interval == 0 {
				interval = 10 * time.Second
			}
			timeout := svc.Healthcheck.Timeout
			if timeout == 0 {
				timeout = 5 * time.Second
			}
			retries := svc.Healthcheck.Retries
			if retries == 0 {
				retries = 3
			}
			startPeriod := svc.Healthcheck.StartPeriod
			if startPeriod == 0 {
				startPeriod = 5 * time.Second
			}

			// Use wget instead of curl (curl not available in slim images)
			// CMD-SHELL form is more reliable than CMD with array
			cfg.Healthcheck = &HealthcheckConfig{
				Test:        []string{"CMD-SHELL", fmt.Sprintf("wget -q --spider http://localhost:%d%s || exit 1", port, svc.Healthcheck.Path)},
				Interval:    interval,
				Timeout:     timeout,
				Retries:     retries,
				StartPeriod: startPeriod,
			}
		}
	}

	return cfg
}
