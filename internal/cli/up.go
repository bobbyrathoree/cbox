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
	upDetach  bool
	upBuild   bool
	upNoDeps  bool
	upTimeout time.Duration
	upEnv     string
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
	upCmd.Flags().DurationVar(&upTimeout, "timeout", 60*time.Second, "startup timeout")
	upCmd.Flags().StringVarP(&upEnv, "env", "e", "", "environment to use (e.g., staging, production)")
}

func runUp(cmd *cobra.Command, args []string) error {
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

	// Apply environment overrides if specified
	if upEnv != "" {
		cfg, err = cfg.WithEnvironment(upEnv)
		if err != nil {
			console.Error("Failed to apply environment %q: %s", upEnv, err)
			return err
		}
		console.Info("Using environment: %s", upEnv)
	}

	console.Header("Starting %s...", cfg.Project.Name)

	// Create orchestrator
	orch := orchestrator.New(cfg, console)

	// Start services
	err = orch.Up(ctx, orchestrator.UpOptions{
		Services: args,
		Build:    upBuild,
		NoDeps:   upNoDeps,
		Detach:   upDetach,
		Timeout:  upTimeout,
	})

	if err != nil {
		return err
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

		// Stream logs from all services
		go streamAllLogs(ctx, cfg, docker, console)

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
func streamAllLogs(ctx context.Context, cfg *config.Config, docker *runtime.Docker, console *output.Console) {
	var wg sync.WaitGroup
	for name := range cfg.Services {
		wg.Add(1)
		go func(svcName string) {
			defer wg.Done()
			containerName := fmt.Sprintf("%s_%s", cfg.Project.Name, svcName)

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
