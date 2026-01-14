// Package orchestrator coordinates multi-service lifecycle.
package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bobbyrathore/cbox/internal/builder"
	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
)

// Orchestrator coordinates multi-service operations.
type Orchestrator struct {
	config  *config.Config
	builder *builder.Builder
	runtime *runtime.Docker
	console *output.Console
}

// New creates a new Orchestrator.
func New(cfg *config.Config, console *output.Console) *Orchestrator {
	return &Orchestrator{
		config:  cfg,
		builder: builder.New(console),
		runtime: runtime.New(console),
		console: console,
	}
}

// UpOptions contains options for bringing up services.
type UpOptions struct {
	Services  []string // Specific services to start (empty = all)
	Build     bool     // Build before starting
	NoDeps    bool     // Don't start dependencies
	Detach    bool     // Run in background
	DevMode   bool     // Development mode with bind mounts
	Timeout   time.Duration
}

// DownOptions contains options for stopping services.
type DownOptions struct {
	Volumes bool // Remove volumes
	Timeout time.Duration
}

// Up starts services in dependency order.
func (o *Orchestrator) Up(ctx context.Context, opts UpOptions) error {
	networkName := fmt.Sprintf("cbox_%s", o.config.Project.Name)

	// Create network
	if err := o.runtime.CreateNetwork(ctx, networkName); err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	// Determine which services to start
	services := opts.Services
	if len(services) == 0 {
		for name := range o.config.Services {
			services = append(services, name)
		}
	}

	// Resolve dependencies and get startup order
	order, err := o.resolveStartOrder(services, !opts.NoDeps)
	if err != nil {
		return err
	}

	o.console.Debug("Startup order: %v", order)

	// Create volumes
	for volName := range o.config.Volumes {
		prefixedName := fmt.Sprintf("%s_%s", o.config.Project.Name, volName)
		if err := o.runtime.CreateVolume(ctx, prefixedName); err != nil {
			return fmt.Errorf("failed to create volume %s: %w", volName, err)
		}
	}

	// Start services in order
	for _, level := range order {
		if err := o.startServiceLevel(ctx, level, networkName, opts); err != nil {
			return err
		}
	}

	return nil
}

// startServiceLevel starts all services in a dependency level (can be parallel).
func (o *Orchestrator) startServiceLevel(ctx context.Context, services []string, network string, opts UpOptions) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(services))

	for _, name := range services {
		wg.Add(1)
		go func(serviceName string) {
			defer wg.Done()

			svc := o.config.Services[serviceName]

			// Check port availability with auto-resolution
			if svc.Port > 0 {
				if err := runtime.CheckPortAvailable(svc.Port); err != nil {
					// Try to find an alternative port
					newPort, findErr := runtime.FindAvailablePort(svc.Port+1, 10)
					if findErr != nil {
						pid, _ := runtime.FindProcessOnPort(svc.Port)
						if pid > 0 {
							errCh <- fmt.Errorf("port %d in use by PID %d and no alternatives available", svc.Port, pid)
						} else {
							errCh <- fmt.Errorf("port %d is in use and no alternatives available", svc.Port)
						}
						return
					}

					// Found alternative port - warn user and use it
					pid, _ := runtime.FindProcessOnPort(svc.Port)
					if pid > 0 {
						o.console.Warn("Port %d in use (PID %d). Using %d instead.", svc.Port, pid, newPort)
					} else {
						o.console.Warn("Port %d in use. Using %d instead.", svc.Port, newPort)
					}
					svc.Port = newPort
					o.config.Services[serviceName] = svc
				}
			}

			// Determine image name
			imageName := svc.Image
			if svc.IsBuildService() {
				imageName = fmt.Sprintf("%s-%s:latest", o.config.Project.Name, serviceName)

				// Build if requested or if image doesn't exist
				if opts.Build || !o.runtime.ImageExists(ctx, imageName) {
					spin := output.NewSpinner(fmt.Sprintf("Building %s...", serviceName), false)
					spin.Start()

					_, err := o.builder.Build(ctx, builder.BuildOptions{
						ServiceName: serviceName,
						Service:     svc,
						ProjectName: o.config.Project.Name,
						DevMode:     opts.DevMode,
					})

					if err != nil {
						spin.Fail(fmt.Sprintf("Failed to build %s", serviceName))
						errCh <- fmt.Errorf("failed to build %s: %w", serviceName, err)
						return
					}
					spin.Success(fmt.Sprintf("Built %s", serviceName))
				}
			} else {
				// Pull image if it doesn't exist
				if !o.runtime.ImageExists(ctx, imageName) {
					spin := output.NewSpinner(fmt.Sprintf("Pulling %s...", imageName), false)
					spin.Start()

					if err := o.runtime.PullImage(ctx, imageName); err != nil {
						spin.Fail(fmt.Sprintf("Failed to pull %s", imageName))
						errCh <- fmt.Errorf("failed to pull %s: %w", imageName, err)
						return
					}
					spin.Success(fmt.Sprintf("Pulled %s", imageName))
				}
			}

			// Remove existing container if it exists
			containerName := fmt.Sprintf("%s_%s", o.config.Project.Name, serviceName)
			o.runtime.RemoveContainer(ctx, containerName)

			// Create container config
			containerCfg := runtime.ContainerConfigFromService(
				serviceName,
				svc,
				o.config.Project.Name,
				network,
				imageName,
				opts.DevMode,
			)

			// Create and start container
			o.console.Debug("Creating container: %s", containerName)
			_, err := o.runtime.CreateContainer(ctx, containerCfg)
			if err != nil {
				errCh <- fmt.Errorf("failed to create %s: %w", serviceName, err)
				return
			}

			if err := o.runtime.StartContainer(ctx, containerName); err != nil {
				errCh <- fmt.Errorf("failed to start %s: %w", serviceName, err)
				return
			}

			// Wait for healthy (if healthcheck configured)
			if svc.HasHealthcheck() {
				o.console.Debug("Waiting for %s to become healthy...", serviceName)
				timeout := opts.Timeout
				if timeout == 0 {
					timeout = 60 * time.Second
				}
				if err := o.runtime.WaitHealthy(ctx, containerName, timeout); err != nil {
					o.console.Warn("%s health check: %s", serviceName, err)
				}
			}

			o.console.Success("Started %s", serviceName)

		}(name)
	}

	wg.Wait()
	close(errCh)

	// Return first error
	for err := range errCh {
		return err
	}

	return nil
}

// Down stops all services.
func (o *Orchestrator) Down(ctx context.Context, opts DownOptions) error {
	networkName := fmt.Sprintf("cbox_%s", o.config.Project.Name)

	// Get all running containers for this project
	containers, err := o.runtime.ListContainers(ctx, map[string]string{
		"cbox.project": o.config.Project.Name,
	}, true)
	if err != nil {
		o.console.Warn("Failed to list containers: %s", err)
	}

	// Stop all containers
	for _, c := range containers {
		o.console.Debug("Stopping %s...", c.Name)
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 10 * time.Second
		}
		o.runtime.StopContainer(ctx, c.Name, timeout)
		o.runtime.RemoveContainer(ctx, c.Name)
		o.console.Success("Stopped %s", c.Name)
	}

	// Remove volumes if requested
	if opts.Volumes {
		for volName := range o.config.Volumes {
			prefixedName := fmt.Sprintf("%s_%s", o.config.Project.Name, volName)
			o.runtime.RemoveVolume(ctx, prefixedName)
			o.console.Success("Removed volume %s", volName)
		}
	}

	// Remove network
	o.runtime.RemoveNetwork(ctx, networkName)

	return nil
}

// resolveStartOrder returns services grouped by dependency level.
// Services in the same level can be started in parallel.
func (o *Orchestrator) resolveStartOrder(services []string, includeDeps bool) ([][]string, error) {
	// Build full dependency set
	toStart := make(map[string]bool)
	for _, s := range services {
		toStart[s] = true
	}

	// Add dependencies if requested
	if includeDeps {
		for changed := true; changed; {
			changed = false
			for name := range toStart {
				svc := o.config.Services[name]
				for _, dep := range svc.DependsOn {
					if !toStart[dep] {
						toStart[dep] = true
						changed = true
					}
				}
			}
		}
	}

	// Topological sort into levels
	// Level 0: services with no dependencies
	// Level 1: services that only depend on level 0
	// etc.

	levels := [][]string{}
	remaining := make(map[string]bool)
	for s := range toStart {
		remaining[s] = true
	}
	started := make(map[string]bool)

	for len(remaining) > 0 {
		level := []string{}

		for name := range remaining {
			svc := o.config.Services[name]
			depsReady := true
			for _, dep := range svc.DependsOn {
				if toStart[dep] && !started[dep] {
					depsReady = false
					break
				}
			}
			if depsReady {
				level = append(level, name)
			}
		}

		if len(level) == 0 {
			// Circular dependency detected
			return nil, fmt.Errorf("circular dependency detected among: %v", remaining)
		}

		for _, name := range level {
			delete(remaining, name)
			started[name] = true
		}

		levels = append(levels, level)
	}

	return levels, nil
}

// Ps lists running services.
func (o *Orchestrator) Ps(ctx context.Context, all bool) ([]ServiceStatus, error) {
	containers, err := o.runtime.ListContainers(ctx, map[string]string{
		"cbox.project": o.config.Project.Name,
	}, all)
	if err != nil {
		return nil, err
	}

	var statuses []ServiceStatus
	for _, c := range containers {
		// Extract service name from container name
		serviceName := c.Name
		prefix := o.config.Project.Name + "_"
		if len(serviceName) > len(prefix) && serviceName[:len(prefix)] == prefix {
			serviceName = serviceName[len(prefix):]
		}

		statuses = append(statuses, ServiceStatus{
			Name:   serviceName,
			Status: c.Status,
			Ports:  c.Ports,
			Health: c.Health,
		})
	}

	return statuses, nil
}

// ServiceStatus represents the status of a service.
type ServiceStatus struct {
	Name   string
	Status string
	Ports  []string
	Health string
}
