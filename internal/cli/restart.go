package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

var (
	restartTimeout time.Duration
)

var restartCmd = &cobra.Command{
	Use:   "restart [service...]",
	Short: "Restart services",
	Long: `Restart one or more services without a full down/up cycle.

This preserves volumes and is faster than stopping and starting.
If no services are specified, all running services are restarted.

Examples:
  cbox restart           # Restart all services
  cbox restart api       # Restart one service
  cbox restart api db    # Restart multiple services
  cbox restart -t 30s    # Restart with custom timeout`,
	RunE: runRestart,
}

func init() {
	restartCmd.Flags().DurationVar(&restartTimeout, "timeout", 10*time.Second, "timeout for stopping each service")
}

func runRestart(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
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

	// Build label filter (with namespace if specified)
	labels := NamespaceLabels(cfg.Project.Name)
	if ns := GetNamespace(); ns != "" {
		console.Info("Using namespace: %s", ns)
	}

	// Get containers for this project
	containers, err := docker.ListContainers(ctx, labels, false) // Only running containers
	if err != nil {
		console.Error("Failed to list containers: %s", err)
		return err
	}

	if len(containers) == 0 {
		console.Warn("No running services found for project %s", cfg.Project.Name)
		console.Info("Run 'cbox up' to start services")
		return nil
	}

	// Build map of service name -> container
	containerMap := make(map[string]runtime.Container)
	for _, c := range containers {
		serviceName := ExtractServiceName(c.Name, cfg.Project.Name)
		containerMap[serviceName] = c
	}

	// Determine which services to restart
	var servicesToRestart []string
	if len(args) > 0 {
		// Restart specific services
		for _, name := range args {
			if _, exists := containerMap[name]; !exists {
				console.Error("Service %s is not running", name)
				return fmt.Errorf("service %s is not running", name)
			}
			servicesToRestart = append(servicesToRestart, name)
		}
	} else {
		// Restart all running services
		for name := range containerMap {
			servicesToRestart = append(servicesToRestart, name)
		}
	}

	if len(servicesToRestart) == 0 {
		console.Info("No services to restart")
		return nil
	}

	console.Header("Restarting %d service(s)...", len(servicesToRestart))

	// Restart each service
	for _, name := range servicesToRestart {
		container := containerMap[name]
		svc := cfg.Services[name]

		spin := output.NewSpinner(fmt.Sprintf("Restarting %s...", name), false)
		spin.Start()

		if err := docker.RestartContainer(ctx, container.Name, restartTimeout); err != nil {
			spin.Fail(fmt.Sprintf("Failed to restart %s", name))
			return fmt.Errorf("failed to restart %s: %w", name, err)
		}

		// Wait for healthy if healthcheck configured
		if svc.HasHealthcheck() {
			if err := docker.WaitHealthy(ctx, container.Name, 60*time.Second); err != nil {
				spin.Fail(fmt.Sprintf("%s restarted but unhealthy", name))
				console.Warn("%s: %s", name, err)
				continue
			}
		}

		spin.Success(fmt.Sprintf("Restarted %s", name))
	}

	console.Success("Restart complete")
	return nil
}
