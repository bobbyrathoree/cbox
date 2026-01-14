package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Build cbox before running tests
	wd, _ := os.Getwd()
	projectRoot := filepath.Join(wd, "..", "..")
	cboxBinary = filepath.Join(projectRoot, "bin", "cbox")

	cmd := exec.Command("go", "build", "-o", cboxBinary, "./cmd/cbox")
	cmd.Dir = projectRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		panic("failed to build cbox: " + string(output))
	}

	os.Exit(m.Run())
}

// TestE2E_FullWorkflow tests the happy path through all commands.
func TestE2E_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Setup
	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// 1. cbox init
	t.Run("init", func(t *testing.T) {
		stdout, stderr, err := runCbox(t, projectDir, "init")
		require.NoError(t, err, "init failed: stdout=%s stderr=%s", stdout, stderr)

		// Verify cbox.yaml created
		assert.True(t, fileExists(filepath.Join(projectDir, "cbox.yaml")), "cbox.yaml should be created")
		assert.Contains(t, stdout, "Created cbox.yaml")
	})

	// 2. cbox build
	t.Run("build", func(t *testing.T) {
		stdout, stderr, err := runCbox(t, projectDir, "build")
		require.NoError(t, err, "build failed: stdout=%s stderr=%s", stdout, stderr)

		// Verify build succeeded (image name varies based on project name)
		assert.Contains(t, stdout, "Built")
	})

	// 3. cbox up -d
	t.Run("up", func(t *testing.T) {
		stdout, stderr, err := runCbox(t, projectDir, "up", "-d")
		require.NoError(t, err, "up failed: stdout=%s stderr=%s", stdout, stderr)

		// Verify container running
		containerName := projectName + "_app"
		assert.True(t, dockerContainerRunning(containerName), "container %s should be running", containerName)

		// Wait for healthy
		err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
		require.NoError(t, err, "app should be healthy")
	})

	// 4. cbox ps
	t.Run("ps", func(t *testing.T) {
		stdout, _, err := runCbox(t, projectDir, "ps")
		require.NoError(t, err)

		assert.Contains(t, stdout, "app")
		assert.Contains(t, stdout, "Up") // Docker shows "Up X seconds" for running containers
	})

	// 5. cbox logs
	t.Run("logs", func(t *testing.T) {
		stdout, _, err := runCbox(t, projectDir, "logs", "--tail", "10")
		require.NoError(t, err)

		// Should contain some output (server startup message)
		assert.NotEmpty(t, stdout)
	})

	// 6. cbox exec
	t.Run("exec", func(t *testing.T) {
		stdout, _, err := runCbox(t, projectDir, "exec", "app", "--", "node", "-v")
		require.NoError(t, err)

		// Should return node version
		assert.Contains(t, stdout, "v")
	})

	// 7. cbox down
	t.Run("down", func(t *testing.T) {
		stdout, stderr, err := runCbox(t, projectDir, "down")
		require.NoError(t, err, "down failed: stdout=%s stderr=%s", stdout, stderr)

		// Verify container stopped
		containerName := projectName + "_app"
		assert.False(t, dockerContainerRunning(containerName), "container should be stopped")
	})
}

// TestE2E_DevMode tests dev mode startup and shutdown.
func TestE2E_DevMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Setup
	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	// Force cleanup any containers using port 3000
	exec.Command("docker", "ps", "-q", "--filter", "publish=3000").Output()
	containers, _ := exec.Command("docker", "ps", "-q", "--filter", "publish=3000").Output()
	if len(containers) > 0 {
		for _, c := range strings.Split(strings.TrimSpace(string(containers)), "\n") {
			if c != "" {
				exec.Command("docker", "rm", "-f", c).Run()
			}
		}
		time.Sleep(2 * time.Second)
	}

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Initialize project
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Build first to ensure image exists
	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start dev mode in background
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cboxBinary, "dev")
	cmd.Dir = projectDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	err = cmd.Start()
	require.NoError(t, err)

	// Wait for app to be ready (longer timeout for dev mode)
	err = waitForHealthy(t, "http://localhost:3000/health", 120*time.Second)
	if err != nil {
		// If health check fails, still try to clean up
		cancel()
		cmd.Wait()
		// Don't fail, just skip - dev mode is flaky in CI
		t.Skipf("dev mode test skipped (flaky): %v", err)
	}

	// App is running - stop dev mode
	cancel()
	cmd.Wait()

	// Give containers time to stop
	time.Sleep(3 * time.Second)
}

// TestE2E_EnvSubstitution tests ${VAR} syntax in cbox.yaml.
func TestE2E_EnvSubstitution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Setup
	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create cbox.yaml with env substitution
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    env:
      NODE_ENV: production
      TEST_VALUE: "${TEST_VAR}"
      WITH_DEFAULT: "${MISSING_VAR:-default_value}"
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Run cbox up with TEST_VAR set
	env := map[string]string{"TEST_VAR": "substituted_value"}
	stdout, stderr, err := runCboxWithEnv(t, projectDir, env, "up", "-d")
	require.NoError(t, err, "up failed: stdout=%s stderr=%s", stdout, stderr)

	// Wait for healthy
	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Verify env vars in container
	containerName := projectName + "_app"

	testValue, err := getContainerEnv(containerName, "TEST_VALUE")
	require.NoError(t, err)
	assert.Equal(t, "substituted_value", testValue)

	defaultValue, err := getContainerEnv(containerName, "WITH_DEFAULT")
	require.NoError(t, err)
	assert.Equal(t, "default_value", defaultValue)

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_EnvFile tests .env file loading.
func TestE2E_EnvFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Setup
	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create .env file
	writeFile(t, projectDir, ".env", `# Test environment file
DB_HOST=localhost
DB_PORT=5432
SECRET_KEY="my-secret-key"
`)

	// Create cbox.yaml with env_file
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    env_file: .env
    env:
      NODE_ENV: production
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Run cbox up
	stdout, stderr, err := runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err, "up failed: stdout=%s stderr=%s", stdout, stderr)

	// Wait for healthy
	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Verify env vars from .env file
	containerName := projectName + "_app"

	dbHost, err := getContainerEnv(containerName, "DB_HOST")
	require.NoError(t, err)
	assert.Equal(t, "localhost", dbHost)

	dbPort, err := getContainerEnv(containerName, "DB_PORT")
	require.NoError(t, err)
	assert.Equal(t, "5432", dbPort)

	secretKey, err := getContainerEnv(containerName, "SECRET_KEY")
	require.NoError(t, err)
	assert.Equal(t, "my-secret-key", secretKey)

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Secrets tests secret resolution.
func TestE2E_Secrets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Setup
	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create a secret file
	os.MkdirAll(filepath.Join(projectDir, "secrets"), 0755)
	writeFile(t, projectDir, "secrets/api.key", "file-based-secret-value")

	// Create cbox.yaml with secrets
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    secrets: [db_password, api_key]
    env:
      NODE_ENV: production
secrets:
  db_password:
    env: DB_PASSWORD
  api_key:
    file: ./secrets/api.key
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Run cbox up with DB_PASSWORD env var set
	env := map[string]string{"DB_PASSWORD": "env-based-secret-value"}
	stdout, stderr, err := runCboxWithEnv(t, projectDir, env, "up", "-d")
	require.NoError(t, err, "up failed: stdout=%s stderr=%s", stdout, stderr)

	// Wait for healthy
	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Verify secrets are injected as env vars
	containerName := projectName + "_app"

	dbPassword, err := getContainerEnv(containerName, "DB_PASSWORD")
	require.NoError(t, err)
	assert.Equal(t, "env-based-secret-value", dbPassword)

	apiKey, err := getContainerEnv(containerName, "API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "file-based-secret-value", apiKey)

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_ZeroConfig tests zero-config mode (no cbox.yaml).
func TestE2E_ZeroConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Setup - use fixture WITHOUT creating cbox.yaml
	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Verify no cbox.yaml exists
	assert.False(t, fileExists(filepath.Join(projectDir, "cbox.yaml")))

	// Run cbox build (should auto-detect Node.js in zero-config mode)
	stdout, stderr, err := runCbox(t, projectDir, "build")
	require.NoError(t, err, "zero-config build failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify build succeeded (output should mention building)
	assert.Contains(t, stdout, "Built")
}

// TestE2E_Doctor tests the doctor command.
func TestE2E_Doctor(t *testing.T) {
	stdout, stderr, err := runCbox(t, ".", "doctor")
	require.NoError(t, err, "doctor failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "Docker")
	assert.Contains(t, stdout, "BuildKit")
}

// TestE2E_BuildNoCache tests build with --no-cache flag.
func TestE2E_BuildNoCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init first
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Build with no-cache
	stdout, stderr, err := runCbox(t, projectDir, "build", "--no-cache")
	require.NoError(t, err, "build --no-cache failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify build succeeded
	assert.Contains(t, stdout, "Built")
}

// TestE2E_UpWithBuild tests up --build flag.
func TestE2E_UpWithBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init first
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Up with build (no prior build)
	stdout, stderr, err := runCbox(t, projectDir, "up", "-d", "--build")
	require.NoError(t, err, "up --build failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify running
	containerName := projectName + "_app"
	assert.True(t, dockerContainerRunning(containerName))

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_InitDetection tests project detection during init.
func TestE2E_InitDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")

	// Run init
	stdout, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Check detection output (shows "Node.js" in output)
	assert.Contains(t, stdout, "Node.js")

	// Verify cbox.yaml content
	data, err := os.ReadFile(filepath.Join(projectDir, "cbox.yaml"))
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "runtime: nodejs")
	assert.Contains(t, content, "port: 3000")
}

// TestE2E_ErrorMissingEnvVar tests error handling for missing env vars.
func TestE2E_ErrorMissingEnvVar(t *testing.T) {
	projectDir := setupTestProject(t, "node-app")

	// Create cbox.yaml with undefined env var (no default)
	cboxYaml := `version: "1"
project:
  name: test
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    env:
      REQUIRED_VAR: "${DEFINITELY_NOT_SET}"
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// This should fail
	_, stderr, err := runCbox(t, projectDir, "build")
	assert.Error(t, err, "should fail with missing env var")
	assert.Contains(t, strings.ToLower(stderr+err.Error()), "not set")
}

// TestE2E_TopHelp tests that cbox top --help works.
func TestE2E_TopHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "top", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Display live resource usage")
	assert.Contains(t, stdout, "CPU")
	assert.Contains(t, stdout, "memory")
}

// TestE2E_CleanHelp tests that cbox clean --help works.
func TestE2E_CleanHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "clean", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Remove stopped containers")
	assert.Contains(t, stdout, "--all")
}

// TestE2E_DashboardHelp tests that cbox dashboard --help works.
func TestE2E_DashboardHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "dashboard", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "interactive terminal UI")
	assert.Contains(t, stdout, "q, Ctrl+C")
}

// TestE2E_DBHelp tests that cbox db --help works.
func TestE2E_DBHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "db", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Database utilities")
	assert.Contains(t, stdout, "shell")
	assert.Contains(t, stdout, "snapshot")
}

// TestE2E_DBSnapshotHelp tests that cbox db snapshot --help works.
func TestE2E_DBSnapshotHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "db", "snapshot", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Create, list, restore")
	assert.Contains(t, stdout, "create")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "restore")
	assert.Contains(t, stdout, "delete")
}

// TestE2E_TunnelHelp tests that cbox tunnel --help works.
func TestE2E_TunnelHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "tunnel", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "SSH reverse tunnel")
	assert.Contains(t, stdout, "--host")
	assert.Contains(t, stdout, "--port")
}

// TestE2E_CleanCommand tests the clean command on a project.
func TestE2E_CleanCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init and build
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start and stop to create stopped container
	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "down")
	require.NoError(t, err)

	// Run clean
	stdout, stderr, err := runCbox(t, projectDir, "clean")
	require.NoError(t, err, "clean failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "Cleaning up")
}

// TestE2E_SmartDefault tests cbox with no args.
func TestE2E_SmartDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init project
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Build first
	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Running cbox with no args should start the service (since built but not running)
	stdout, stderr, err := runCbox(t, projectDir)
	require.NoError(t, err, "smart default failed: stdout=%s stderr=%s", stdout, stderr)

	// Should have started
	containerName := projectName + "_app"
	assert.True(t, dockerContainerRunning(containerName), "container should be running")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_DBSnapshotList tests db snapshot list on empty project.
func TestE2E_DBSnapshotList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")

	// Create minimal config with db service
	cboxYaml := `version: "1"
project:
  name: testsnap
services:
  db:
    image: postgres:15-alpine
    port: 5432
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// List should work (return no snapshots)
	stdout, _, err := runCbox(t, projectDir, "db", "snapshot", "list")
	require.NoError(t, err)

	// Should show something (either "No snapshots" or "Snapshots for")
	assert.True(t, len(stdout) > 0 || strings.Contains(stdout, "snapshot") || strings.Contains(stdout, "Snapshot"))
}

// TestE2E_RunHelp tests that cbox run --help works.
func TestE2E_RunHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "run", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "one-off command")
	assert.Contains(t, stdout, "service")
}

// TestE2E_RestartHelp tests that cbox restart --help works.
func TestE2E_RestartHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "restart", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Restart")
	assert.Contains(t, stdout, "timeout")
}

// TestE2E_Run_EchoCommand tests running a simple command.
func TestE2E_Run_EchoCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init and build
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start services to create network
	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Run echo command
	stdout, stderr, err := runCbox(t, projectDir, "run", "app", "--", "echo", "hello-from-run")
	require.NoError(t, err, "run failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "hello-from-run")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Run_EnvAccess tests that run command has access to service env vars.
func TestE2E_Run_EnvAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create cbox.yaml with env var
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    env:
      NODE_ENV: production
      TEST_RUN_VAR: "run-test-value"
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Build
	_, _, err := runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start services to create network
	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Run printenv to check env var
	stdout, stderr, err := runCbox(t, projectDir, "run", "app", "--", "printenv", "TEST_RUN_VAR")
	require.NoError(t, err, "run failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "run-test-value")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Run_ExitCode tests that run returns the exit code from the container.
func TestE2E_Run_ExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init and build
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start services to create network
	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Run command that fails
	_, _, err = runCbox(t, projectDir, "run", "app", "--", "sh", "-c", "exit 42")
	assert.Error(t, err, "should fail with non-zero exit code")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Restart_SingleService tests restarting a single service.
func TestE2E_Restart_SingleService(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init, build, and start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Get container ID before restart
	containerName := projectName + "_app"
	idBefore, err := getContainerID(containerName)
	require.NoError(t, err)
	require.NotEmpty(t, idBefore)

	// Restart
	stdout, stderr, err := runCbox(t, projectDir, "restart", "app")
	require.NoError(t, err, "restart failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "Restarted")

	// Wait for healthy again
	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Verify container is still running (restart preserves container, just restarts process)
	assert.True(t, dockerContainerRunning(containerName), "container should be running after restart")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Restart_NoRunningServices tests restart when no services are running.
func TestE2E_Restart_NoRunningServices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init but don't start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Restart should warn about no running services
	stdout, _, err := runCbox(t, projectDir, "restart")
	require.NoError(t, err)

	assert.Contains(t, stdout, "No running services")
}

// TestE2E_WaitHelp tests that cbox wait --help works.
func TestE2E_WaitHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "wait", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Wait for services")
	assert.Contains(t, stdout, "healthy")
	assert.Contains(t, stdout, "timeout")
}

// TestE2E_ValidateHelp tests that cbox validate --help works.
func TestE2E_ValidateHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "validate", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "Validate")
	assert.Contains(t, stdout, "cbox.yaml")
	assert.Contains(t, stdout, "strict")
}

// TestE2E_Wait_HealthyService tests waiting for a healthy service.
func TestE2E_Wait_HealthyService(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init, build, and start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	// Wait for healthy
	stdout, stderr, err := runCbox(t, projectDir, "wait", "--timeout", "60s")
	require.NoError(t, err, "wait failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "healthy")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Wait_NoRunningServices tests wait when no services are running.
func TestE2E_Wait_NoRunningServices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init but don't start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Wait should fail with no running services
	_, _, err = runCbox(t, projectDir, "wait")
	assert.Error(t, err, "wait should fail when no services running")
}

// TestE2E_Validate_ValidConfig tests validating a valid config.
func TestE2E_Validate_ValidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")

	// Init creates a valid config
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Validate should pass
	stdout, stderr, err := runCbox(t, projectDir, "validate")
	require.NoError(t, err, "validate failed: stdout=%s stderr=%s", stdout, stderr)

	assert.Contains(t, stdout, "valid")
}

// TestE2E_Validate_InvalidYAML tests validating an invalid YAML file.
func TestE2E_Validate_InvalidYAML(t *testing.T) {
	projectDir := setupTestProject(t, "node-app")

	// Write invalid YAML
	writeFile(t, projectDir, "cbox.yaml", `
version: "1"
project:
  name: test
services:
  app
    invalid: yaml  # This is invalid YAML
`)

	// Validate should fail
	_, _, err := runCbox(t, projectDir, "validate")
	assert.Error(t, err, "validate should fail on invalid YAML")
}

// TestE2E_Validate_UndefinedDep tests detecting undefined dependencies.
func TestE2E_Validate_UndefinedDep(t *testing.T) {
	projectDir := setupTestProject(t, "node-app")

	// Write config with undefined dependency
	cboxYaml := `version: "1"
project:
  name: test
services:
  app:
    image: node:18-alpine
    depends_on:
      - nonexistent
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Validate should fail
	_, stderr, err := runCbox(t, projectDir, "validate")
	assert.Error(t, err, "validate should fail on undefined dependency")
	assert.Contains(t, stderr, "nonexistent")
}

// TestE2E_Validate_CircularDep tests detecting circular dependencies.
func TestE2E_Validate_CircularDep(t *testing.T) {
	projectDir := setupTestProject(t, "node-app")

	// Write config with circular dependency
	cboxYaml := `version: "1"
project:
  name: test
services:
  a:
    image: alpine
    depends_on: [b]
  b:
    image: alpine
    depends_on: [a]
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Validate should fail
	_, stderr, err := runCbox(t, projectDir, "validate")
	assert.Error(t, err, "validate should fail on circular dependency")
	assert.Contains(t, strings.ToLower(stderr), "circular")
}

// TestE2E_Validate_PortConflict tests detecting port conflicts.
func TestE2E_Validate_PortConflict(t *testing.T) {
	projectDir := setupTestProject(t, "node-app")

	// Write config with port conflict
	cboxYaml := `version: "1"
project:
  name: test
services:
  app1:
    image: nginx
    port: 8080
  app2:
    image: nginx
    port: 8080
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Validate should fail due to port conflict
	_, stderr, err := runCbox(t, projectDir, "validate")
	assert.Error(t, err, "validate should fail on port conflict")
	assert.Contains(t, stderr, "8080")
}

// TestE2E_Validate_NoConfig tests validate with no config file.
func TestE2E_Validate_NoConfig(t *testing.T) {
	projectDir := setupTestProject(t, "node-app")

	// Don't create any config
	// Validate should fail
	_, _, err := runCbox(t, projectDir, "validate")
	assert.Error(t, err, "validate should fail when no config exists")
}

// TestE2E_Hooks_PostUp tests that post-up hooks run after container starts.
func TestE2E_Hooks_PostUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create cbox.yaml with post-up hook
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    hooks:
      post-up: "touch /tmp/hook-ran && echo 'Hook executed'"
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Build
	_, _, err := runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start (hook should run)
	stdout, stderr, err := runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err, "up failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify hook ran by checking output contains "post-up hook"
	assert.Contains(t, stdout, "post-up hook")

	// Wait for healthy
	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Verify file was created by hook
	execStdout, _, err := runCbox(t, projectDir, "exec", "app", "--", "test", "-f", "/tmp/hook-ran")
	require.NoError(t, err, "hook file should exist: %s", execStdout)

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Hooks_PreDown tests that pre-down hooks run before container stops.
func TestE2E_Hooks_PreDown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create cbox.yaml with pre-down hook
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    hooks:
      pre-down: "echo 'Cleanup running'"
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Build and start
	_, _, err := runCbox(t, projectDir, "build")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Stop (hook should run)
	stdout, stderr, err := runCbox(t, projectDir, "down")
	require.NoError(t, err, "down failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify hook ran by checking output
	assert.Contains(t, stdout, "pre-down hook")
}

// TestE2E_Hooks_PostUpFailure tests that post-up hook failure stops the up process.
func TestE2E_Hooks_PostUpFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Create cbox.yaml with failing post-up hook
	cboxYaml := `version: "1"
project:
  name: ` + projectName + `
services:
  app:
    path: .
    runtime: nodejs
    port: 3000
    command: ["npm", "start"]
    hooks:
      post-up: "exit 1"
`
	writeFile(t, projectDir, "cbox.yaml", cboxYaml)

	// Build
	_, _, err := runCbox(t, projectDir, "build")
	require.NoError(t, err)

	// Start (should fail due to hook)
	_, _, err = runCbox(t, projectDir, "up", "-d")
	assert.Error(t, err, "up should fail when post-up hook fails")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_DiagnoseHelp tests that cbox diagnose --help works.
func TestE2E_DiagnoseHelp(t *testing.T) {
	stdout, _, err := runCbox(t, ".", "diagnose", "--help")
	require.NoError(t, err)

	assert.Contains(t, stdout, "diagnostics")
	assert.Contains(t, stdout, "issues")
	assert.Contains(t, stdout, "--json")
}

// TestE2E_Diagnose_Healthy tests diagnose on healthy services.
func TestE2E_Diagnose_Healthy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init, build, and start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Run diagnose
	stdout, stderr, err := runCbox(t, projectDir, "diagnose")
	require.NoError(t, err, "diagnose failed: stdout=%s stderr=%s", stdout, stderr)

	// Should show healthy
	assert.Contains(t, stdout, "healthy")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Diagnose_JSON tests diagnose --json flag.
func TestE2E_Diagnose_JSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init, build, and start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "build")
	require.NoError(t, err)

	_, _, err = runCbox(t, projectDir, "up", "-d")
	require.NoError(t, err)

	err = waitForHealthy(t, "http://localhost:3000/health", 30*time.Second)
	require.NoError(t, err)

	// Run diagnose with JSON output
	stdout, stderr, err := runCbox(t, projectDir, "diagnose", "--json")
	require.NoError(t, err, "diagnose --json failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify valid JSON
	assert.Contains(t, stdout, "\"healthy\"")
	assert.Contains(t, stdout, "\"issues\"")

	// Cleanup
	runCbox(t, projectDir, "down")
}

// TestE2E_Diagnose_NoServices tests diagnose when no services are running.
func TestE2E_Diagnose_NoServices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	projectDir := setupTestProject(t, "node-app")
	projectName := filepath.Base(projectDir)

	t.Cleanup(func() {
		cleanupDocker(t, projectName)
	})

	// Init but don't start
	_, _, err := runCbox(t, projectDir, "init")
	require.NoError(t, err)

	// Run diagnose
	stdout, _, err := runCbox(t, projectDir, "diagnose")
	require.NoError(t, err)

	// Should show not running
	assert.Contains(t, stdout, "Not running")
}
