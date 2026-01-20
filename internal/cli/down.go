package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/bobbyrathore/cbox/internal/orchestrator"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var (
	downVolumes bool
	downTimeout time.Duration
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop all services",
	Long: `Stop all running services and remove containers.

Examples:
  cbox down            Stop all services
  cbox down --volumes  Stop and remove volumes`,
	RunE: runDown,
}

func init() {
	downCmd.Flags().BoolVar(&downVolumes, "volumes", false, "remove volumes")
	downCmd.Flags().DurationVar(&downTimeout, "timeout", 10*time.Second, "shutdown timeout")
}

func runDown(cmd *cobra.Command, args []string) error {
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

	console.Header("Stopping %s...", cfg.Project.Name)

	// Create orchestrator (with namespace if specified)
	var orch *orchestrator.Orchestrator
	ns := GetNamespace()
	if ns != "" {
		console.Info("Using namespace: %s", ns)
		orch = orchestrator.NewWithNamespace(cfg, console, ns)
	} else {
		orch = orchestrator.New(cfg, console)
	}

	// Stop services
	err = orch.Down(ctx, orchestrator.DownOptions{
		Volumes: downVolumes,
		Timeout: downTimeout,
	})

	if err != nil {
		return err
	}

	console.Newline()
	console.Success("All services stopped")

	return nil
}
