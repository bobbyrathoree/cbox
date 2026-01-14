package cli

import (
	"context"
	"fmt"

	"github.com/bobbyrathore/cbox/internal/dev"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var (
	devNoSync bool
	devPort   int
)

var devCmd = &cobra.Command{
	Use:   "dev [service...]",
	Short: "Start development mode with hot reload",
	Long: `Start services in development mode with file watching and hot reload.

Source files are bind-mounted into containers, allowing instant
hot module replacement via nodemon/vite/next dev.

Examples:
  cbox dev           Start all services in dev mode
  cbox dev api       Start specific service`,
	RunE: runDev,
}

func init() {
	devCmd.Flags().BoolVar(&devNoSync, "no-sync", false, "rebuild instead of sync on changes")
	devCmd.Flags().IntVar(&devPort, "port", 0, "override port for service")
}

func runDev(cmd *cobra.Command, args []string) error {
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

	// Create dev loop
	devLoop := dev.New(cfg, console)

	// Start dev mode
	return devLoop.Start(ctx, dev.Options{
		Services: args,
		NoSync:   devNoSync,
	})
}
