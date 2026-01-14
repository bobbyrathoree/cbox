package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var (
	runNoTTY   bool
	runWorkdir string
	runUser    string
)

var runCmd = &cobra.Command{
	Use:   "run <service> -- <command>",
	Short: "Run a one-off command in a service context",
	Long: `Run a one-off command using the same image, environment, and network as a service.

The container is automatically removed after the command completes.
Use this for migrations, seeds, scripts, or any one-off tasks.

Examples:
  cbox run api -- npm run migrate
  cbox run api -- node scripts/seed.js
  cbox run db -- psql -U postgres -c "SELECT 1"
  cbox run api -- sh -c "echo \$NODE_ENV"`,
	Args:               cobra.MinimumNArgs(1),
	RunE:               runRun,
	DisableFlagParsing: false,
}

func init() {
	runCmd.Flags().BoolVar(&runNoTTY, "no-tty", false, "disable pseudo-TTY allocation")
	runCmd.Flags().StringVarP(&runWorkdir, "workdir", "w", "", "working directory inside container")
	runCmd.Flags().StringVarP(&runUser, "user", "u", "", "run as specified user")
}

func runRun(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	// Parse args: service name is first arg, command is everything after --
	serviceName := args[0]
	var command []string

	// Find the command after --
	// Cobra handles -- specially, so args after -- are in ArgsLenAtDash
	dashIdx := cmd.ArgsLenAtDash()
	if dashIdx >= 0 && dashIdx < len(args) {
		command = args[dashIdx:]
	} else if len(args) > 1 {
		// No --, assume everything after service name is the command
		command = args[1:]
	}

	if len(command) == 0 {
		console.Error("No command specified")
		console.Info("Usage: cbox run <service> -- <command>")
		return fmt.Errorf("no command specified")
	}

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Check service exists
	svc, ok := cfg.Services[serviceName]
	if !ok {
		console.Error("Unknown service: %s", serviceName)
		console.Info("Available services: ")
		for name := range cfg.Services {
			console.Info("  - %s", name)
		}
		return fmt.Errorf("unknown service: %s", serviceName)
	}

	// Determine image name
	imageName := svc.Image
	if svc.IsBuildService() {
		imageName = fmt.Sprintf("%s-%s:latest", cfg.Project.Name, serviceName)
	}

	// Build docker run command
	networkName := fmt.Sprintf("cbox_%s", cfg.Project.Name)
	dockerArgs := []string{"run", "--rm"}

	// Allocate TTY by default unless disabled or not a terminal
	if !runNoTTY && isTerminal() {
		dockerArgs = append(dockerArgs, "-it")
	} else if !runNoTTY {
		dockerArgs = append(dockerArgs, "-i")
	}

	// Network - use project network so container can reach other services
	dockerArgs = append(dockerArgs, "--network", networkName)

	// Environment variables
	for k, v := range svc.Env {
		dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Working directory
	if runWorkdir != "" {
		dockerArgs = append(dockerArgs, "-w", runWorkdir)
	}

	// User
	if runUser != "" {
		dockerArgs = append(dockerArgs, "-u", runUser)
	}

	// Labels for identification
	dockerArgs = append(dockerArgs, "--label", fmt.Sprintf("cbox.project=%s", cfg.Project.Name))
	dockerArgs = append(dockerArgs, "--label", fmt.Sprintf("cbox.service=%s", serviceName))
	dockerArgs = append(dockerArgs, "--label", "cbox.run=true")

	// Image
	dockerArgs = append(dockerArgs, imageName)

	// Command
	dockerArgs = append(dockerArgs, command...)

	// Execute
	console.Debug("Running: docker %v", dockerArgs)
	execCmd := exec.Command("docker", dockerArgs...)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	err = execCmd.Run()
	if err != nil {
		// Return the exit code from the container if available
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}

	return nil
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
