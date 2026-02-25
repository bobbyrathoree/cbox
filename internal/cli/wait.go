package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

var (
	waitTimeout time.Duration
)

var waitCmd = &cobra.Command{
	Use:   "wait [service...]",
	Short: "Wait for services to be healthy",
	Long: `Wait for services to be running and healthy.

This command blocks until all specified services (or all services if none specified)
are healthy, or until the timeout is reached.

Exit codes:
  0 - All services healthy
  1 - Timeout reached or error

Examples:
  cbox wait                    # Wait for all services
  cbox wait api db             # Wait for specific services
  cbox wait --timeout 60s      # Wait with custom timeout
  cbox wait --timeout 2m api   # Wait 2 minutes for api`,
	RunE: runWait,
}

func init() {
	waitCmd.Flags().DurationVarP(&waitTimeout, "timeout", "t", 60*time.Second, "timeout for waiting")
}

func runWait(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()

	console := output.NewWithOptions(verbose, quiet)

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	docker := runtime.New(console)

	// Get containers for this project
	containers, err := docker.ListContainers(ctx, NamespaceLabels(cfg.Project.Name), false) // Only running containers
	if err != nil {
		console.Error("Failed to list containers: %s", err)
		return err
	}

	if len(containers) == 0 {
		console.Error("No running services found for project %s", cfg.Project.Name)
		console.Info("Run 'cbox up' to start services first")
		return fmt.Errorf("no running services")
	}

	// Build map of service name -> container name
	serviceContainers := make(map[string]string)
	for _, c := range containers {
		serviceName := c.Name
		prefix := ProjectPrefix(cfg.Project.Name) + "_"
		if len(serviceName) > len(prefix) && serviceName[:len(prefix)] == prefix {
			serviceName = serviceName[len(prefix):]
		}
		serviceContainers[serviceName] = c.Name
	}

	// Determine which services to wait for
	var servicesToWait []string
	if len(args) > 0 {
		// Wait for specific services
		for _, name := range args {
			if _, exists := serviceContainers[name]; !exists {
				console.Error("Service %s is not running", name)
				return fmt.Errorf("service %s is not running", name)
			}
			servicesToWait = append(servicesToWait, name)
		}
	} else {
		// Wait for all running services
		for name := range serviceContainers {
			servicesToWait = append(servicesToWait, name)
		}
	}

	if len(servicesToWait) == 0 {
		console.Info("No services to wait for")
		return nil
	}

	console.Info("Waiting for %d service(s) to be healthy...", len(servicesToWait))

	// Wait for all services in parallel
	var wg sync.WaitGroup
	errCh := make(chan error, len(servicesToWait))
	resultCh := make(chan string, len(servicesToWait))

	for _, name := range servicesToWait {
		wg.Add(1)
		go func(serviceName string) {
			defer wg.Done()

			containerName := serviceContainers[serviceName]
			svc := cfg.Services[serviceName]

			// Determine wait timeout for this service
			timeout := waitTimeout
			if svc.Healthcheck.Timeout > 0 && svc.Healthcheck.Retries > 0 {
				// Use healthcheck config to estimate timeout
				estimatedTime := time.Duration(svc.Healthcheck.Retries) * (svc.Healthcheck.Interval + svc.Healthcheck.Timeout)
				if estimatedTime > timeout {
					timeout = estimatedTime
				}
			}

			if err := docker.WaitHealthy(ctx, containerName, timeout); err != nil {
				errCh <- fmt.Errorf("%s: %w", serviceName, err)
				return
			}

			resultCh <- serviceName
		}(name)
	}

	// Wait for all goroutines
	wg.Wait()
	close(errCh)
	close(resultCh)

	// Collect results
	var healthy []string
	for name := range resultCh {
		healthy = append(healthy, name)
	}

	// Check for errors
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}

	// Report results
	if len(healthy) > 0 {
		for _, name := range healthy {
			console.Success("%s is healthy", name)
		}
	}

	if len(errors) > 0 {
		for _, err := range errors {
			console.Error("%s", err)
		}
		return fmt.Errorf("some services failed health check")
	}

	console.Success("All services healthy")
	return nil
}
