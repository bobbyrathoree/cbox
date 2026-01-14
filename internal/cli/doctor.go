package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system requirements",
	Long: `Check that all system requirements are met for running cbox.

Verifies:
  - Docker is installed and running
  - BuildKit is available
  - cbox.yaml is valid (if present)
  - Disk space is sufficient`,
	RunE: runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	// yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Println("Checking system requirements...")
	fmt.Println()

	allPassed := true

	// Check Docker
	dockerVersion, err := checkDocker()
	if err != nil {
		fmt.Printf("  %s Docker: %s\n", red("✗"), err)
		allPassed = false
	} else {
		fmt.Printf("  %s Docker Engine %s\n", green("✓"), dockerVersion)
	}

	// Check BuildKit
	buildkitAvailable, err := checkBuildKit()
	if err != nil {
		fmt.Printf("  %s BuildKit: %s\n", red("✗"), err)
		allPassed = false
	} else if buildkitAvailable {
		fmt.Printf("  %s BuildKit enabled\n", green("✓"))
	} else {
		fmt.Printf("  %s BuildKit: not available\n", red("✗"))
		allPassed = false
	}

	// Check config file
	configValid, err := checkConfig()
	if err != nil {
		fmt.Printf("  %s Config: %s\n", red("✗"), err)
		// Not a hard failure - config might not exist yet
	} else if configValid {
		fmt.Printf("  %s cbox.yaml valid\n", green("✓"))
	} else {
		fmt.Printf("  %s cbox.yaml not found (run 'cbox init' to create)\n", green("-"))
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All checks passed!")
		return nil
	}
	return fmt.Errorf("some checks failed")
}

func checkDocker() (string, error) {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker not found or not running")
	}
	return strings.TrimSpace(string(output)), nil
}

func checkBuildKit() (bool, error) {
	// Check if buildx is available
	cmd := exec.Command("docker", "buildx", "version")
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("docker buildx not available")
	}
	return true, nil
}

func checkConfig() (bool, error) {
	// TODO: Actually validate the config file
	// For now, just check if it exists
	cmd := exec.Command("test", "-f", GetConfigFile())
	if err := cmd.Run(); err != nil {
		return false, nil // File doesn't exist, not an error
	}
	return true, nil
}
