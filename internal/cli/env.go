package cli

import (
	"fmt"
	"sort"

	cboxctx "github.com/bobbyrathore/cbox/internal/context"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

// EnvListResultJSON is the data payload for env list JSON output
type EnvListResultJSON struct {
	Environments []string `json:"environments"`
	Current      string   `json:"current"`
}

// EnvCurrentResultJSON is the data payload for env current JSON output
type EnvCurrentResultJSON struct {
	Environment string `json:"environment"`
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment context",
	Long: `Manage environment context for the current project.

The active environment is persisted in .cbox/env and automatically
applied to all commands (up, build, dev, etc.).

Subcommands:
  list       List available environments
  switch     Switch to a different environment
  current    Show the current environment

Examples:
  cbox env list             List environments from cbox.yaml
  cbox env switch staging   Switch to staging environment
  cbox env switch --clear   Clear environment (use defaults)
  cbox env current          Show current environment`,
	RunE: runEnvCurrent, // Default to showing current
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	Long: `List all environments defined in cbox.yaml.

Shows each environment and indicates which one is currently active.

Example:
  cbox env list`,
	RunE: runEnvList,
}

var envSwitchCmd = &cobra.Command{
	Use:   "switch [environment]",
	Short: "Switch to an environment",
	Long: `Switch to a different environment.

The active environment will be used for all subsequent commands
(build, up, dev, etc.) until switched again or cleared.

Examples:
  cbox env switch staging    Switch to staging
  cbox env switch production Switch to production
  cbox env switch --clear    Clear and use defaults`,
	Args: cobra.MaximumNArgs(1),
	RunE: runEnvSwitch,
}

var envCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show current environment",
	Long: `Show the currently active environment.

Example:
  cbox env current`,
	RunE: runEnvCurrent,
}

var envShowCmd = &cobra.Command{
	Use:   "show <environment>",
	Short: "Show environment configuration",
	Long: `Show the configuration overrides for a specific environment.

Displays what settings will change when you switch to this environment.

Examples:
  cbox env show staging      Show staging environment overrides
  cbox env show production   Show production environment overrides`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvShow,
}

var envClearFlag bool

func init() {
	envSwitchCmd.Flags().BoolVar(&envClearFlag, "clear", false, "clear active environment")

	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envSwitchCmd)
	envCmd.AddCommand(envCurrentCmd)
	envCmd.AddCommand(envShowCmd)
}

func runEnvList(cmd *cobra.Command, args []string) error {
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	cfg, err := loadConfigRaw()
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("env list", err)
			return err // Return error for proper exit code
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	currentEnv := cboxctx.GetCurrentEnv()

	// Sort environment names for consistent output
	var envNames []string
	for name := range cfg.Environments {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)

	// JSON output mode
	if console.IsJSONMode() {
		result := EnvListResultJSON{
			Environments: envNames,
			Current:      currentEnv,
		}
		console.EmitJSON("env list", result, nil)
		return nil
	}

	// Text output mode
	if len(cfg.Environments) == 0 {
		console.Info("No environments defined in cbox.yaml")
		console.Newline()
		console.Dim("Define environments like this:")
		console.Dim("")
		console.Dim("  environments:")
		console.Dim("    staging:")
		console.Dim("      services:")
		console.Dim("        api:")
		console.Dim("          env:")
		console.Dim("            DEBUG: \"true\"")
		console.Dim("    production:")
		console.Dim("      services:")
		console.Dim("        api:")
		console.Dim("          env:")
		console.Dim("            DEBUG: \"false\"")
		return nil
	}

	console.Header("Environments for %s", cfg.Project.Name)
	console.Newline()

	for _, name := range envNames {
		if name == currentEnv {
			console.Success("  * %s (active)", name)
		} else {
			console.Info("    %s", name)
		}
	}

	console.Newline()
	console.Dim("Switch environment: cbox env switch <name>")

	return nil
}

func runEnvSwitch(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	// Handle --clear flag
	if envClearFlag {
		if err := cboxctx.ClearCurrentEnv(); err != nil {
			console.Error("Failed to clear environment: %s", err)
			return err
		}
		console.Success("Environment cleared (using defaults)")
		return nil
	}

	// Require environment name
	if len(args) == 0 {
		console.ErrorWithHint(
			"Environment name required",
			"Usage: cbox env switch <environment> or cbox env switch --clear",
		)
		return fmt.Errorf("environment name required")
	}

	envName := args[0]

	// Validate environment exists in config
	// Use loadConfigRaw() to avoid failing when stored env has unset vars
	cfg, err := loadConfigRaw()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	if !cfg.HasEnvironment(envName) {
		// List available environments
		var available []string
		for name := range cfg.Environments {
			available = append(available, name)
		}
		sort.Strings(available)

		hint := "No environments defined in cbox.yaml"
		if len(available) > 0 {
			hint = fmt.Sprintf("Available environments: %v", available)
		}

		console.ErrorWithHint(
			fmt.Sprintf("Environment '%s' not found", envName),
			hint,
		)
		return fmt.Errorf("environment '%s' not found", envName)
	}

	// Set the environment
	if err := cboxctx.SetCurrentEnv(envName); err != nil {
		console.Error("Failed to switch environment: %s", err)
		return err
	}

	console.Success("Switched to environment: %s", envName)
	console.Newline()
	console.Dim("This will be used for all subsequent commands.")
	console.Dim("Clear with: cbox env switch --clear")

	return nil
}

func runEnvCurrent(cmd *cobra.Command, args []string) error {
	currentEnv := cboxctx.GetCurrentEnv()
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	// JSON output mode
	if console.IsJSONMode() {
		result := EnvCurrentResultJSON{
			Environment: currentEnv,
		}
		console.EmitJSON("env current", result, nil)
		return nil
	}

	// In quiet mode, just output the env name (for scripting)
	// If no env set, output nothing (empty = defaults)
	if quiet {
		if currentEnv != "" {
			fmt.Println(currentEnv)
		}
		return nil
	}

	// Normal mode - verbose output
	if currentEnv == "" {
		console.Info("No environment set (using defaults)")
		console.Newline()
		console.Dim("Set environment: cbox env switch <name>")
		console.Dim("List available:  cbox env list")
	} else {
		console.Info("Current environment: %s", currentEnv)
	}

	return nil
}

func runEnvShow(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)
	envName := args[0]

	// Use loadConfigRaw() to avoid failing when stored env has unset vars
	cfg, err := loadConfigRaw()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	if !cfg.HasEnvironment(envName) {
		// List available environments
		var available []string
		for name := range cfg.Environments {
			available = append(available, name)
		}
		sort.Strings(available)

		hint := "No environments defined in cbox.yaml"
		if len(available) > 0 {
			hint = fmt.Sprintf("Available environments: %v", available)
		}

		console.ErrorWithHint(
			fmt.Sprintf("Environment '%s' not found", envName),
			hint,
		)
		return fmt.Errorf("environment '%s' not found", envName)
	}

	envConfig := cfg.Environments[envName]

	console.Header("Environment: %s", envName)
	console.Newline()

	if len(envConfig.Services) == 0 {
		console.Dim("  No service overrides defined")
		return nil
	}

	for svcName, overrides := range envConfig.Services {
		console.Info("  Service: %s", svcName)

		if len(overrides.Env) > 0 {
			console.Dim("    Environment variables:")
			// Sort keys for consistent output
			var keys []string
			for k := range overrides.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				// Show raw value (may contain ${VAR})
				console.Info("      %s: %s", k, overrides.Env[k])
			}
		}

		if overrides.Deploy != nil {
			console.Dim("    Deploy overrides:")
			if overrides.Deploy.CPU > 0 {
				console.Info("      cpu: %d", overrides.Deploy.CPU)
			}
			if overrides.Deploy.Memory > 0 {
				console.Info("      memory: %d", overrides.Deploy.Memory)
			}
			if overrides.Deploy.DesiredCount > 0 {
				console.Info("      replicas: %d", overrides.Deploy.DesiredCount)
			}
			if overrides.Deploy.HealthCheckPath != "" {
				console.Info("      health_check_path: %s", overrides.Deploy.HealthCheckPath)
			}
		}
		console.Newline()
	}

	return nil
}
