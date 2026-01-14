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
	upDetach  bool
	upBuild   bool
	upNoDeps  bool
	upTimeout time.Duration
)

var upCmd = &cobra.Command{
	Use:   "up [service...]",
	Short: "Start all services",
	Long: `Start one or more services defined in cbox.yaml.

If no services are specified, starts all services respecting
dependency order.

Examples:
  cbox up              Start all services
  cbox up -d           Start in background
  cbox up api --no-deps Start without dependencies`,
	RunE: runUp,
}

func init() {
	upCmd.Flags().BoolVarP(&upDetach, "detach", "d", false, "run in background")
	upCmd.Flags().BoolVar(&upBuild, "build", false, "build images before starting")
	upCmd.Flags().BoolVar(&upNoDeps, "no-deps", false, "don't start dependencies")
	upCmd.Flags().DurationVar(&upTimeout, "timeout", 60*time.Second, "startup timeout")
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
		// TODO: Stream logs in foreground mode
		select {}
	}

	return nil
}
