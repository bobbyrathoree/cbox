package e2e

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// cboxBinary is the path to the cbox binary.
var cboxBinary string

func init() {
	// Build cbox binary for testing
	wd, _ := os.Getwd()
	// Navigate to project root (tests/e2e -> project root)
	projectRoot := filepath.Join(wd, "..", "..")
	cboxBinary = filepath.Join(projectRoot, "bin", "cbox")
}

// setupTestProject copies the fixture to a temp directory and returns the path.
func setupTestProject(t *testing.T, fixtureName string) string {
	t.Helper()

	// Get fixture path
	wd, err := os.Getwd()
	require.NoError(t, err)
	fixturePath := filepath.Join(wd, "testdata", fixtureName)

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "cbox-e2e-*")
	require.NoError(t, err)

	// Copy fixture to temp directory
	err = copyDir(fixturePath, tempDir)
	require.NoError(t, err)

	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})

	return tempDir
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			return err
		}

		return os.Chmod(dstPath, info.Mode())
	})
}

// runCbox executes cbox with the given arguments in the specified directory.
func runCbox(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	cmd := exec.Command(cboxBinary, args...)
	cmd.Dir = dir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// runCboxWithEnv executes cbox with environment variables.
func runCboxWithEnv(t *testing.T, dir string, env map[string]string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	cmd := exec.Command(cboxBinary, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// cleanupDocker removes containers, networks, and images for a project.
func cleanupDocker(t *testing.T, projectName string) {
	t.Helper()

	// Stop and remove containers
	containers := listContainers(projectName)
	for _, c := range containers {
		exec.Command("docker", "rm", "-f", c).Run()
	}

	// Remove network
	networkName := fmt.Sprintf("cbox_%s", projectName)
	exec.Command("docker", "network", "rm", networkName).Run()

	// Remove images
	imageName := fmt.Sprintf("%s_app:latest", projectName)
	exec.Command("docker", "rmi", "-f", imageName).Run()
}

// listContainers returns container names for a project.
func listContainers(projectName string) []string {
	cmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("label=cbox.project=%s", projectName), "--format", "{{.Names}}")
	output, _ := cmd.Output()

	var containers []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			containers = append(containers, line)
		}
	}
	return containers
}

// waitForHealthy polls a URL until it returns 200 or times out.
func waitForHealthy(t *testing.T, url string, timeout time.Duration) error {
	t.Helper()

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for %s to be healthy", url)
}

// dockerContainerExists checks if a container exists.
func dockerContainerExists(name string) bool {
	cmd := exec.Command("docker", "inspect", name)
	return cmd.Run() == nil
}

// dockerContainerRunning checks if a container is running.
func dockerContainerRunning(name string) bool {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// dockerImageExists checks if an image exists.
func dockerImageExists(tag string) bool {
	cmd := exec.Command("docker", "image", "inspect", tag)
	return cmd.Run() == nil
}

// dockerNetworkExists checks if a network exists.
func dockerNetworkExists(name string) bool {
	cmd := exec.Command("docker", "network", "inspect", name)
	return cmd.Run() == nil
}

// getContainerEnv gets an environment variable from a running container.
func getContainerEnv(containerName, envKey string) (string, error) {
	cmd := exec.Command("docker", "exec", containerName, "printenv", envKey)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// writeFile creates a file with the given content in the specified directory.
func writeFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buildCbox builds the cbox binary if it doesn't exist.
func buildCbox(t *testing.T) {
	t.Helper()

	if fileExists(cboxBinary) {
		return
	}

	wd, _ := os.Getwd()
	projectRoot := filepath.Join(wd, "..", "..")

	cmd := exec.Command("go", "build", "-o", cboxBinary, "./cmd/cbox")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build cbox: %s", string(output))
}
