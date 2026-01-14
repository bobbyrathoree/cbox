package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/bobbyrathore/cbox/internal/orchestrator"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var psAll bool

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running services",
	Long: `List all running services and their status.

Examples:
  cbox ps         List running services
  cbox ps --all   Include stopped services`,
	RunE: runPs,
}

func init() {
	psCmd.Flags().BoolVar(&psAll, "all", false, "show all services (including stopped)")
}

func runPs(cmd *cobra.Command, args []string) error {
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

	// Create orchestrator
	orch := orchestrator.New(cfg, console)

	// Get service statuses
	statuses, err := orch.Ps(ctx, psAll)
	if err != nil {
		return err
	}

	if len(statuses) == 0 {
		console.Info("No services running")
		console.Info("Run 'cbox up' to start services")
		return nil
	}

	// Print table
	headers := []string{"NAME", "STATUS", "PORTS", "HEALTH"}
	rows := [][]string{}

	for _, s := range statuses {
		ports := strings.Join(s.Ports, ", ")
		if ports == "" {
			ports = "-"
		}
		health := s.Health
		if health == "" {
			health = "-"
		}
		rows = append(rows, []string{s.Name, s.Status, ports, health})
	}

	console.Table(headers, rows)

	return nil
}
