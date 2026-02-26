package cli

import (
	"context"
	"fmt"
	"sort"
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
			return err // Return error for proper exit code
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Validate service names if provided
	if len(args) > 0 {
		for _, arg := range args {
			if _, ok := cfg.Services[arg]; !ok {
				available := make([]string, 0, len(cfg.Services))
				for name := range cfg.Services {
					available = append(available, name)
				}
				sort.Strings(available)
				return fmt.Errorf("unknown service '%s' (available: %s)", arg, strings.Join(available, ", "))
			}
		}
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
			return err // Return error for proper exit code
		}
		return err
	}

	// Filter statuses if specific services were requested
	if len(args) > 0 {
		requested := make(map[string]bool, len(args))
		for _, a := range args {
			requested[a] = true
		}
		filtered := statuses[:0]
		for _, s := range statuses {
			if requested[s.Name] {
				filtered = append(filtered, s)
			}
		}
		statuses = filtered
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
