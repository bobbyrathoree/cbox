package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/orchestrator"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

var (
	upDetach   bool
	upBuild    bool
	upNoDeps   bool
	upAutoPort bool
	upTimeout  time.Duration
	upEnv      string
)

var upCmd = &cobra.Command{
	Use:   "up [service...]",
	Short: "Start all services",
	Long: `Start one or more services defined in cbox.yaml.

If no services are specified, starts all services respecting
dependency order.

Examples:
  cbox up                  Start all services
  cbox up -d               Start in background
  cbox up --env staging    Start with staging environment
  cbox up api --no-deps    Start without dependencies`,
	RunE: runUp,
}

func init() {
	upCmd.Flags().BoolVarP(&upDetach, "detach", "d", false, "run in background")
	upCmd.Flags().BoolVar(&upBuild, "build", false, "build images before starting")
	upCmd.Flags().BoolVar(&upNoDeps, "no-deps", false, "don't start dependencies")
	upCmd.Flags().BoolVar(&upAutoPort, "auto-port", false, "auto-find alternative port if configured port is in use")
	upCmd.Flags().DurationVar(&upTimeout, "timeout", 60*time.Second, "startup timeout")
	upCmd.Flags().StringVarP(&upEnv, "env", "e", "", "environment to use (e.g., staging, production)")
}

func runUp(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	// In JSON mode, force detached mode (no log streaming)
	if console.IsJSONMode() {
		upDetach = true
	}

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("up", err)
			return err
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Apply environment overrides if specified
	if upEnv != "" {
		cfg, err = cfg.WithEnvironment(upEnv)
		if err != nil {
			if console.IsJSONMode() {
				console.EmitJSONError("up", err)
				return err
			}
			console.Error("Failed to apply environment %q: %s", upEnv, err)
			return err
		}
		console.Info("Using environment: %s", upEnv)
	}

	console.Header("Starting %s...", cfg.Project.Name)

	// Create orchestrator (with namespace if specified)
	var orch *orchestrator.Orchestrator
	ns := GetNamespace()
	if ns != "" {
		console.Info("Using namespace: %s", ns)
		orch = orchestrator.NewWithNamespace(cfg, console, ns)
	} else {
		orch = orchestrator.New(cfg, console)
	}

	// Start services
	err = orch.Up(ctx, orchestrator.UpOptions{
		Services: args,
		Build:    upBuild,
		NoDeps:   upNoDeps,
		Detach:   upDetach,
		AutoPort: upAutoPort,
		Timeout:  upTimeout,
	})

	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("up", err)
		}
		return err
	}

	if console.IsJSONMode() {
		console.EmitJSON("up", map[string]interface{}{"started": true}, nil)
		return nil
	}

	console.Newline()
	console.Success("All services started")

	if !upDetach {
		console.Info("Press Ctrl+C to stop")

		// Setup signal handling for graceful shutdown
		ctx, cancel := context.WithCancel(ctx)
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			console.Newline()
			console.Info("Shutting down...")
			cancel()
		}()

		// Create docker runtime for log streaming
		docker := runtime.New(console)

		// Stream logs from all services (pass namespace for container name resolution)
		go streamAllLogs(ctx, cfg, docker, console, orch.GetNamespace())

		// Wait for shutdown signal
		<-ctx.Done()

		// Cleanup - stop listening for signals and shut down services
		signal.Stop(sigCh)
		console.Newline()
		orch.Down(context.Background(), orchestrator.DownOptions{})
	}

	return nil
}

// streamAllLogs streams logs from all services concurrently.
func streamAllLogs(ctx context.Context, cfg *config.Config, docker runtime.ContainerRuntime, console *output.Console, namespace string) {
	var wg sync.WaitGroup
	for name := range cfg.Services {
		wg.Add(1)
		go func(svcName string) {
			defer wg.Done()
			// Build container name with optional namespace prefix
			projectPrefix := cfg.Project.Name
			if namespace != "" {
				projectPrefix = fmt.Sprintf("%s-%s", namespace, cfg.Project.Name)
			}
			containerName := fmt.Sprintf("%s_%s", projectPrefix, svcName)

			// Get log reader with follow mode
			reader, err := docker.ContainerLogs(ctx, containerName, true, 10)
			if err != nil {
				return
			}
			defer reader.Close()

			// Read and output logs line by line
			scanner := bufio.NewScanner(reader)
			for scanner.Scan() {
				select {
				case <-ctx.Done():
					return
				default:
					console.ServiceLog(svcName, scanner.Text())
				}
			}
		}(name)
	}
	wg.Wait()
}
