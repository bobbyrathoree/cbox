package cli

import (
	"fmt"

	"github.com/bobbyrathore/cbox/internal/dashboard"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:     "dashboard",
	Aliases: []string{"dash", "ui"},
	Short:   "Interactive TUI dashboard",
	Long: `Launch an interactive terminal UI for managing services.

Features:
  - Live service status view
  - Start/stop/restart services with keyboard shortcuts
  - Navigate with vim-style keys (j/k) or arrows

Keyboard shortcuts:
  q, Ctrl+C, Esc - Quit
  j, Down        - Move down
  k, Up          - Move up
  r              - Restart selected service
  s              - Stop selected service
  u              - Start selected service

Examples:
  cbox dashboard     Launch the dashboard
  cbox dash          Short alias
  cbox ui            Another alias`,
	RunE: runDashboard,
}

func init() {
	// No flags needed
}

func runDashboard(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	// Load config
	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Run the dashboard
	return dashboard.Run(cfg, console)
}
