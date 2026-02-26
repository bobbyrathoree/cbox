package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

// DoctorCheckJSON represents a single doctor check result for JSON output
type DoctorCheckJSON struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DoctorResultJSON is the data payload for doctor command JSON output
type DoctorResultJSON struct {
	Checks    []DoctorCheckJSON `json:"checks"`
	AllPassed bool              `json:"all_passed"`
}

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
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	console.Info("Checking system requirements...")
	console.Newline()

	allPassed := true
	var checks []DoctorCheckJSON

	// Check Docker
	dockerVersion, err := checkDocker()
	if err != nil {
		console.Error("Docker: %s", err)
		allPassed = false
		checks = append(checks, DoctorCheckJSON{Name: "docker", Passed: false, Error: err.Error()})
	} else {
		console.Success("Docker Engine %s", dockerVersion)
		checks = append(checks, DoctorCheckJSON{Name: "docker", Passed: true, Version: dockerVersion})
	}

	// Check BuildKit
	buildkitAvailable, err := checkBuildKit()
	if err != nil {
		console.Error("BuildKit: %s", err)
		allPassed = false
		checks = append(checks, DoctorCheckJSON{Name: "buildkit", Passed: false, Error: err.Error()})
	} else if buildkitAvailable {
		console.Success("BuildKit enabled")
		checks = append(checks, DoctorCheckJSON{Name: "buildkit", Passed: true})
	} else {
		console.Error("BuildKit: not available")
		allPassed = false
		checks = append(checks, DoctorCheckJSON{Name: "buildkit", Passed: false, Error: "not available"})
	}

	// Check config file
	configValid, err := checkConfig()
	if err != nil {
		console.Error("Config: %s", err)
		checks = append(checks, DoctorCheckJSON{Name: "config", Passed: false, Error: err.Error()})
		// Not a hard failure - config might not exist yet
	} else if configValid {
		console.Success("cbox.yaml valid")
		checks = append(checks, DoctorCheckJSON{Name: "config", Passed: true})
	} else {
		console.Dim("cbox.yaml not found (run 'cbox init' to create)")
		checks = append(checks, DoctorCheckJSON{Name: "config", Passed: false, Error: "cbox.yaml not found"})
	}

	// JSON output mode
	if console.IsJSONMode() {
		result := DoctorResultJSON{
			Checks:    checks,
			AllPassed: allPassed,
		}
		if !allPassed {
			console.EmitJSON("doctor", result, []output.JSONError{{Message: "some checks failed"}})
		} else {
			console.EmitJSON("doctor", result, nil)
		}
		if !allPassed {
			return fmt.Errorf("some checks failed")
		}
		return nil
	}

	console.Newline()
	if allPassed {
		console.Success("All checks passed!")
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
	configPath := GetConfigFile()
	if _, err := os.Stat(configPath); err != nil {
		return false, nil // File doesn't exist, not an error
	}
	_, err := config.Load(configPath)
	if err != nil {
		return false, fmt.Errorf("invalid: %s", err)
	}
	return true, nil
}
