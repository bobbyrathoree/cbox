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

	"github.com/bobbyrathore/cbox/internal/config"
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

// TopServiceJSON represents a single service's stats for JSON output
type TopServiceJSON struct {
	Name    string `json:"name"`
	CPU     string `json:"cpu"`
	Memory  string `json:"memory"`
	NetIO   string `json:"net_io"`
	BlockIO string `json:"block_io"`
	PIDs    string `json:"pids"`
}

// TopResultJSON is the data payload for top command JSON output
type TopResultJSON struct {
	Services []TopServiceJSON `json:"services"`
}

func runTop(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	// Load config to get project name
	cfg, err := loadConfig()
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("top", err)
			return err
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// JSON mode: get one snapshot (--no-stream), emit JSON, return
	if console.IsJSONMode() {
		return runTopJSON(ctx, console, cfg)
	}

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Get container names for this project (with optional namespace filter)
	statsArgs := []string{"stats",
		"--format", `{"Name":"{{.Name}}","CPUPerc":"{{.CPUPerc}}","MemUsage":"{{.MemUsage}}","NetIO":"{{.NetIO}}","BlockIO":"{{.BlockIO}}","PIDs":"{{.PIDs}}"}`,
		"--filter", fmt.Sprintf("label=cbox.project=%s", cfg.Project.Name),
	}
	if ns := GetNamespace(); ns != "" {
		statsArgs = append(statsArgs, "--filter", fmt.Sprintf("label=cbox.namespace=%s", ns))
	}

	// Start docker stats streaming
	statsCmd := exec.CommandContext(ctx, "docker", statsArgs...)

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

// runTopJSON gets a single snapshot of stats and emits JSON
func runTopJSON(ctx context.Context, console *output.Console, cfg *config.Config) error {
	statsArgs := []string{"stats", "--no-stream",
		"--format", `{"Name":"{{.Name}}","CPUPerc":"{{.CPUPerc}}","MemUsage":"{{.MemUsage}}","NetIO":"{{.NetIO}}","BlockIO":"{{.BlockIO}}","PIDs":"{{.PIDs}}"}`,
		"--filter", fmt.Sprintf("label=cbox.project=%s", cfg.Project.Name),
	}
	if ns := GetNamespace(); ns != "" {
		statsArgs = append(statsArgs, "--filter", fmt.Sprintf("label=cbox.namespace=%s", ns))
	}

	statsCmd := exec.CommandContext(ctx, "docker", statsArgs...)
	out, err := statsCmd.Output()
	if err != nil {
		console.EmitJSONError("top", fmt.Errorf("failed to get docker stats: %w", err))
		return err
	}

	var services []TopServiceJSON
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var stats ContainerStats
		if err := json.Unmarshal([]byte(line), &stats); err != nil {
			continue
		}
		services = append(services, TopServiceJSON{
			Name:    extractServiceName(stats.Name, cfg.Project.Name),
			CPU:     stats.CPU,
			Memory:  stats.Memory,
			NetIO:   stats.NetIO,
			BlockIO: stats.BlockIO,
			PIDs:    stats.PIDs,
		})
	}

	console.EmitJSON("top", TopResultJSON{Services: services}, nil)
	return nil
}

func extractServiceName(containerName, projectName string) string {
	return ExtractServiceName(containerName, projectName)
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
