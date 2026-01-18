package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bobbyrathore/cbox/internal/compose"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Docker Compose utilities",
	Long: `Docker Compose utilities for converting compose files to cbox format.

Subcommands:
  import    Convert docker-compose.yaml to cbox.yaml

Examples:
  cbox compose import                  Import docker-compose.yaml
  cbox compose import -f compose.yml   Import specific file
  cbox compose import --force          Overwrite existing cbox.yaml`,
}

var composeImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Convert docker-compose.yaml to cbox.yaml",
	Long: `Convert a docker-compose.yaml file to cbox.yaml format.

This command parses a docker-compose v3.x file and generates an equivalent
cbox.yaml configuration file.

Supported mappings:
  - image          -> image
  - build/context  -> path
  - ports          -> port, expose
  - environment    -> env
  - depends_on     -> depends_on
  - volumes        -> volumes
  - command        -> command
  - healthcheck    -> healthcheck

Examples:
  cbox compose import                     Import docker-compose.yaml
  cbox compose import -f docker-compose.prod.yml
  cbox compose import --output app.yaml   Specify output file
  cbox compose import --force             Overwrite existing output`,
	RunE: runComposeImport,
}

var (
	composeInputFile  string
	composeOutputFile string
	composeForce      bool
)

func init() {
	composeImportCmd.Flags().StringVarP(&composeInputFile, "file", "f", "", "input docker-compose file (default: docker-compose.yaml)")
	composeImportCmd.Flags().StringVarP(&composeOutputFile, "output", "o", "cbox.yaml", "output cbox config file")
	composeImportCmd.Flags().BoolVar(&composeForce, "force", false, "overwrite existing output file")

	composeCmd.AddCommand(composeImportCmd)
}

func runComposeImport(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	// Find input file
	inputFile := composeInputFile
	if inputFile == "" {
		// Try common compose file names
		candidates := []string{
			"docker-compose.yaml",
			"docker-compose.yml",
			"compose.yaml",
			"compose.yml",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				inputFile = c
				break
			}
		}
		if inputFile == "" {
			console.ErrorWithHint(
				"No docker-compose file found",
				"Specify a file with: cbox compose import -f <file>",
			)
			return fmt.Errorf("compose file not found")
		}
	}

	// Check if input exists
	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		console.ErrorWithHint(
			fmt.Sprintf("File not found: %s", inputFile),
			"Check the file path and try again",
		)
		return fmt.Errorf("input file not found: %s", inputFile)
	}

	// Check if output exists
	if _, err := os.Stat(composeOutputFile); err == nil && !composeForce {
		console.ErrorWithHint(
			fmt.Sprintf("Output file already exists: %s", composeOutputFile),
			"Use --force to overwrite or --output to specify a different file",
		)
		return fmt.Errorf("output file exists (use --force to overwrite)")
	}

	console.Info("Importing %s...", inputFile)

	// Parse compose file
	composeFile, err := compose.Parse(inputFile)
	if err != nil {
		console.Error("Failed to parse compose file: %s", err)
		return err
	}

	// Infer project name from directory
	absPath, _ := filepath.Abs(inputFile)
	projectName := compose.InferProjectName(absPath)

	// Convert to cbox config
	cfg := compose.Convert(composeFile, projectName)

	// Generate YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		console.Error("Failed to generate cbox config: %s", err)
		return err
	}

	// Add header comment
	header := fmt.Sprintf("# Generated from %s by cbox compose import\n# Review and adjust as needed\n\n", inputFile)

	// Write output
	if err := os.WriteFile(composeOutputFile, []byte(header+string(data)), 0644); err != nil {
		console.Error("Failed to write output file: %s", err)
		return err
	}

	console.Success("Created %s", composeOutputFile)
	console.Newline()

	// Print summary
	console.Info("Imported %d services:", len(cfg.Services))
	for name, svc := range cfg.Services {
		if svc.Path != "" {
			console.Dim("  - %s (build: %s)", name, svc.Path)
		} else {
			console.Dim("  - %s (image: %s)", name, svc.Image)
		}
	}

	if len(cfg.Volumes) > 0 {
		console.Info("Imported %d volumes", len(cfg.Volumes))
	}

	console.Newline()
	console.Dim("Next steps:")
	console.Dim("  1. Review %s and adjust settings", composeOutputFile)
	console.Dim("  2. Run 'cbox validate' to check configuration")
	console.Dim("  3. Run 'cbox up' to start services")

	return nil
}
