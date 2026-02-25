package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var (
	execInteractive bool
	execTTY         bool
	execWorkdir     string
)

var execCmd = &cobra.Command{
	Use:   "exec <service> <command...>",
	Short: "Execute command in service container",
	Long: `Execute a command inside a running service container.

Examples:
  cbox exec api npm test     Run npm test in api container
  cbox exec -it api sh       Interactive shell`,
	Args: cobra.MinimumNArgs(2),
	RunE: runExec,
}

func init() {
	execCmd.Flags().BoolVarP(&execInteractive, "interactive", "i", false, "keep STDIN open")
	execCmd.Flags().BoolVarP(&execTTY, "tty", "t", false, "allocate pseudo-TTY")
	execCmd.Flags().StringVarP(&execWorkdir, "workdir", "w", "", "working directory")
}

func runExec(cmd *cobra.Command, args []string) error {
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

	service := args[0]
	command := args[1:]

	// Check service exists
	if _, ok := cfg.Services[service]; !ok {
		console.Error("Unknown service: %s", service)
		return fmt.Errorf("unknown service: %s", service)
	}

	containerName := fmt.Sprintf("%s_%s", ProjectPrefix(cfg.Project.Name), service)

	// Build docker exec command
	dockerArgs := []string{"exec"}
	if execInteractive {
		dockerArgs = append(dockerArgs, "-i")
	}
	if execTTY {
		dockerArgs = append(dockerArgs, "-t")
	}
	if execWorkdir != "" {
		dockerArgs = append(dockerArgs, "-w", execWorkdir)
	}
	dockerArgs = append(dockerArgs, containerName)
	dockerArgs = append(dockerArgs, command...)

	// Execute
	execCmd := exec.Command("docker", dockerArgs...)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
}
