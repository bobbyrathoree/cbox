package cli

import (
	"fmt"
	"os"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var (
	validateStrict bool
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate cbox.yaml configuration",
	Long: `Validate the cbox.yaml configuration file without running anything.

This command checks for:
  - Valid YAML syntax
  - Required fields present
  - Dependencies reference existing services
  - No circular dependencies
  - Secrets reference existing secrets
  - Volumes reference existing volumes
  - Port conflicts between services

Exit codes:
  0 - Configuration is valid
  1 - Configuration has errors

Examples:
  cbox validate              # Validate cbox.yaml
  cbox validate --strict     # Also check for warnings`,
	RunE: runValidate,
}

func init() {
	validateCmd.Flags().BoolVar(&validateStrict, "strict", false, "treat warnings as errors")
}

func runValidate(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	configPath := GetConfigFile()

	// Check if config exists
	if !config.Exists(configPath) {
		console.Error("Configuration file not found: %s", configPath)
		console.Info("Run 'cbox init' to create a configuration file")
		return fmt.Errorf("config file not found")
	}

	// Try to load config (this validates YAML, env vars, secrets, dependencies)
	cfg, err := config.Load(configPath)
	if err != nil {
		console.Error("Configuration invalid")
		console.Error("  %s", err)
		return err
	}

	// Additional validation checks
	var warnings []string
	var errors []string

	// Check for port conflicts between services
	portUsage := make(map[int][]string)
	for name, svc := range cfg.Services {
		if svc.Port > 0 {
			portUsage[svc.Port] = append(portUsage[svc.Port], name)
		}
		for _, p := range svc.Expose {
			portUsage[p] = append(portUsage[p], name)
		}
	}

	for port, services := range portUsage {
		if len(services) > 1 {
			errors = append(errors, fmt.Sprintf("Port %d is used by multiple services: %v", port, services))
		}
	}

	// Check for services without healthchecks (warning)
	for name, svc := range cfg.Services {
		if svc.Port > 0 && !svc.HasHealthcheck() {
			warnings = append(warnings, fmt.Sprintf("Service '%s' exposes port %d but has no healthcheck defined", name, svc.Port))
		}
	}

	// Check for build services without runtime specified (warning)
	for name, svc := range cfg.Services {
		if svc.IsBuildService() && svc.Runtime == "" {
			warnings = append(warnings, fmt.Sprintf("Service '%s' builds from source but has no runtime specified", name))
		}
	}

	// Check image services have valid image format
	for name, svc := range cfg.Services {
		if svc.IsImageService() {
			if svc.Image == "" {
				errors = append(errors, fmt.Sprintf("Service '%s' has empty image", name))
			}
		}
	}

	// Check for empty commands on build services (warning)
	for name, svc := range cfg.Services {
		if svc.IsBuildService() && len(svc.Command) == 0 && len(svc.Dev.Command) == 0 {
			// This is okay for Dockerfiles that specify CMD, just a note
			if verbose {
				warnings = append(warnings, fmt.Sprintf("Service '%s' has no command specified (relies on Dockerfile CMD)", name))
			}
		}
	}

	// Check if any secrets are defined but not used
	usedSecrets := make(map[string]bool)
	for _, svc := range cfg.Services {
		for _, s := range svc.Secrets {
			usedSecrets[s] = true
		}
	}
	for name := range cfg.Secrets {
		if !usedSecrets[name] {
			warnings = append(warnings, fmt.Sprintf("Secret '%s' is defined but not used by any service", name))
		}
	}

	// Check if any volumes are defined but not used
	usedVolumes := make(map[string]bool)
	for _, svc := range cfg.Services {
		for _, vol := range svc.Volumes {
			// Extract volume name (before the colon)
			if vol[0] != '.' && vol[0] != '/' {
				volName := vol
				for i, c := range vol {
					if c == ':' {
						volName = vol[:i]
						break
					}
				}
				usedVolumes[volName] = true
			}
		}
	}
	for name := range cfg.Volumes {
		if !usedVolumes[name] {
			warnings = append(warnings, fmt.Sprintf("Volume '%s' is defined but not used by any service", name))
		}
	}

	// Report results
	if len(errors) > 0 {
		console.Error("Configuration has errors:")
		for _, e := range errors {
			console.Error("  ✗ %s", e)
		}
	}

	if len(warnings) > 0 {
		if validateStrict {
			console.Error("Configuration has warnings (strict mode):")
		} else {
			console.Warn("Configuration has warnings:")
		}
		for _, w := range warnings {
			console.Warn("  ⚠ %s", w)
		}
	}

	// Determine exit status
	hasErrors := len(errors) > 0
	hasWarnings := len(warnings) > 0

	if hasErrors || (validateStrict && hasWarnings) {
		os.Exit(1)
	}

	// Success message
	console.Success("Configuration valid")
	console.Info("  Project: %s", cfg.Project.Name)
	console.Info("  Services: %d", len(cfg.Services))
	if len(cfg.Volumes) > 0 {
		console.Info("  Volumes: %d", len(cfg.Volumes))
	}
	if len(cfg.Secrets) > 0 {
		console.Info("  Secrets: %d", len(cfg.Secrets))
	}

	return nil
}
