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

// ServiceStatusJSON represents a service status for JSON output
type ServiceStatusJSON struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Ports  []string `json:"ports"`
	Health string   `json:"health"`
}

// PsResultJSON is the data payload for ps command JSON output
type PsResultJSON struct {
	Services []ServiceStatusJSON `json:"services"`
}

func runPs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("ps", err)
			return nil // Don't return error in JSON mode, it's in the output
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Create orchestrator (with namespace if specified)
	var orch *orchestrator.Orchestrator
	ns := GetNamespace()
	if ns != "" {
		orch = orchestrator.NewWithNamespace(cfg, console, ns)
	} else {
		orch = orchestrator.New(cfg, console)
	}

	// Get service statuses
	statuses, err := orch.Ps(ctx, psAll)
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("ps", err)
			return nil
		}
		return err
	}

	// JSON output mode
	if console.IsJSONMode() {
		result := PsResultJSON{Services: make([]ServiceStatusJSON, 0, len(statuses))}
		for _, s := range statuses {
			health := s.Health
			if health == "" {
				health = "-"
			}
			result.Services = append(result.Services, ServiceStatusJSON{
				Name:   s.Name,
				Status: s.Status,
				Ports:  s.Ports,
				Health: health,
			})
		}
		console.EmitJSON("ps", result, nil)
		return nil
	}

	// Text output mode
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
