package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bobbyrathore/cbox/internal/orchestrator"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

// Build-time variables (set via ldflags)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Global flags
var (
	configFile string
	verbose    bool
	quiet      bool
)

var rootCmd = &cobra.Command{
	Use:   "cbox",
	Short: "Fast container workflow engine",
	Long: `cbox - Fast container workflow engine

A fast, opinionated container workflow engine that unifies building,
running, and developing multi-service applications.

Get started:
  cbox init       Initialize a new project
  cbox dev        Start development mode with hot reload
  cbox up         Start all services
  cbox down       Stop all services`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runDefault,
}

// runDefault implements smart default behavior when cbox is run with no args
func runDefault(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOptions(verbose, quiet)

	// Try to load config
	cfg, err := loadConfig()
	if err != nil {
		// No config found - show help
		return cmd.Help()
	}

	// Initialize runtime and orchestrator
	docker := runtime.New(console)
	orch := orchestrator.New(cfg, console)

	// Check service states
	statuses, _ := orch.Ps(ctx, true) // Include stopped containers

	anyRunning := false
	allBuilt := true

	// Check running containers
	for _, s := range statuses {
		if strings.Contains(strings.ToLower(s.Status), "up") {
			anyRunning = true
		}
	}

	// Check if images exist for build services
	for name, svc := range cfg.Services {
		if svc.IsBuildService() {
			imageName := fmt.Sprintf("%s_%s:latest", cfg.Project.Name, name)
			if !docker.ImageExists(ctx, imageName) {
				allBuilt = false
				break
			}
		}
	}

	// Smart routing based on state
	switch {
	case anyRunning:
		// Services running - show status
		console.Info("Services are running:")
		console.Newline()
		return runPs(cmd, args)

	case allBuilt:
		// Images exist but not running - start them
		console.Header("Starting %s...", cfg.Project.Name)
		// Force detach mode when running via smart default
		upDetach = true
		return runUp(cmd, args)

	default:
		// No images - build and start
		console.Header("Building and starting %s...", cfg.Project.Name)

		// Build first
		if err := runBuild(cmd, args); err != nil {
			return err
		}

		// Force detach mode when running via smart default
		upDetach = true
		// Then start
		return runUp(cmd, args)
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("cbox %s\n", Version)
		if verbose {
			fmt.Printf("  commit:  %s\n", Commit)
			fmt.Printf("  built:   %s\n", BuildTime)
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "cbox.yaml", "config file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet output (errors only)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(devCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(topCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(tunnelCmd)
}

// Execute runs the root command
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

// GetConfigFile returns the config file path
func GetConfigFile() string {
	return configFile
}

// IsVerbose returns whether verbose mode is enabled
func IsVerbose() bool {
	return verbose
}

// IsQuiet returns whether quiet mode is enabled
func IsQuiet() bool {
	return quiet
}
