package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bobbyrathore/cbox/internal/diagnose"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

var (
	diagnoseJSON bool
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Diagnose common issues",
	Long: `Run diagnostics on running services to identify common issues.

Checks performed:
  - Container crash loops (restart count)
  - Health check failures
  - High memory usage
  - Port conflicts/remapping
  - Missing dependencies
  - Connection errors in logs

Examples:
  cbox diagnose          # Run diagnostics
  cbox diagnose --json   # Output as JSON`,
	RunE: runDiagnose,
}

func init() {
	diagnoseCmd.Flags().BoolVar(&diagnoseJSON, "json", false, "output as JSON")
}

func runDiagnose(cmd *cobra.Command, args []string) error {
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

	console.Header("Diagnosing %s...", cfg.Project.Name)

	// Run diagnostics
	result, err := diagnose.Diagnose(ctx, cfg, docker)
	if err != nil {
		console.Error("Diagnosis failed: %s", err)
		return err
	}

	// Output JSON if requested
	if diagnoseJSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// Output human-readable format
	console.Newline()

	// Show issues
	if len(result.Issues) > 0 {
		console.Error("Issues found:")
		console.Newline()

		for _, issue := range result.Issues {
			switch issue.Severity {
			case diagnose.SeverityError:
				console.Error("  ✗ [%s] %s", issue.Service, issue.Message)
			case diagnose.SeverityWarning:
				console.Warn("  ⚠ [%s] %s", issue.Service, issue.Message)
			default:
				console.Info("  ℹ [%s] %s", issue.Service, issue.Message)
			}

			if issue.Details != "" {
				// Indent and truncate details
				lines := truncateLines(issue.Details, 3)
				for _, line := range lines {
					console.Dim("      %s", line)
				}
			}

			if issue.Suggestion != "" {
				console.Info("      → %s", issue.Suggestion)
			}

			console.Newline()
		}
	}

	// Show not running services
	if len(result.NotRunning) > 0 {
		console.Warn("Not running:")
		for _, name := range result.NotRunning {
			console.Warn("  - %s", name)
		}
		console.Newline()
	}

	// Show healthy services
	if len(result.Healthy) > 0 {
		console.Success("%d service(s) healthy:", len(result.Healthy))
		for _, name := range result.Healthy {
			console.Success("  ✓ %s", name)
		}
	}

	return nil
}

// truncateLines splits a string into lines and limits the number returned.
func truncateLines(s string, max int) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			if current != "" {
				lines = append(lines, current)
			}
			current = ""
			if len(lines) >= max {
				break
			}
		} else {
			current += string(c)
		}
	}
	if current != "" && len(lines) < max {
		lines = append(lines, current)
	}
	return lines
}
