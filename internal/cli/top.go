package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Display live resource usage",
	Long: `Display live resource usage for running services.

Shows CPU, memory, and network I/O for each container.
Press Ctrl+C to stop.

Examples:
  cbox top           Show resource usage for all services`,
	RunE: runTop,
}

func init() {
	// No flags needed for now
}

// ContainerStats holds parsed docker stats output
type ContainerStats struct {
	Name     string `json:"Name"`
	CPU      string `json:"CPUPerc"`
	Memory   string `json:"MemUsage"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     string `json:"PIDs"`
}

func runTop(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	console := output.NewWithOptions(verbose, quiet)

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Load config to get project name
	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Get container names for this project
	containerFilter := fmt.Sprintf("label=cbox.project=%s", cfg.Project.Name)

	// Start docker stats streaming
	statsCmd := exec.CommandContext(ctx, "docker", "stats",
		"--format", `{"Name":"{{.Name}}","CPUPerc":"{{.CPUPerc}}","MemUsage":"{{.MemUsage}}","NetIO":"{{.NetIO}}","BlockIO":"{{.BlockIO}}","PIDs":"{{.PIDs}}"}`,
		"--filter", containerFilter,
	)

	stdout, err := statsCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}

	if err := statsCmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker stats: %w", err)
	}

	console.Header("Resource usage for %s", cfg.Project.Name)
	console.Dim("Press Ctrl+C to stop")
	console.Newline()

	// Parse and display stats
	scanner := bufio.NewScanner(stdout)
	var currentStats []ContainerStats
	lastPrint := time.Now()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var stats ContainerStats
		if err := json.Unmarshal([]byte(line), &stats); err != nil {
			continue
		}

		// Extract service name from container name
		stats.Name = extractServiceName(stats.Name, cfg.Project.Name)
		currentStats = append(currentStats, stats)

		// Print every second (docker stats outputs all containers then repeats)
		if time.Since(lastPrint) > 800*time.Millisecond && len(currentStats) > 0 {
			clearScreen()
			printStats(console, cfg.Project.Name, currentStats)
			currentStats = nil
			lastPrint = time.Now()
		}
	}

	statsCmd.Wait()
	return nil
}

func extractServiceName(containerName, projectName string) string {
	prefix := projectName + "_"
	if strings.HasPrefix(containerName, prefix) {
		return containerName[len(prefix):]
	}
	return containerName
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func printStats(console *output.Console, projectName string, stats []ContainerStats) {
	console.Header("Resource usage for %s", projectName)
	console.Dim("Press Ctrl+C to stop")
	console.Newline()

	// Table header
	headers := []string{"SERVICE", "CPU", "MEMORY", "NET I/O", "BLOCK I/O", "PIDS"}
	var rows [][]string

	for _, s := range stats {
		rows = append(rows, []string{
			s.Name,
			s.CPU,
			s.Memory,
			s.NetIO,
			s.BlockIO,
			s.PIDs,
		})
	}

	if len(rows) == 0 {
		console.Warn("No running containers found")
		return
	}

	console.Table(headers, rows)
	console.Newline()
	console.Dim("Updated: %s", time.Now().Format("15:04:05"))
}
