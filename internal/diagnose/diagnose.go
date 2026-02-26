// Package diagnose provides smart problem detection for cbox services.
package diagnose

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/runtime"
)

// Severity levels for issues
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Issue represents a diagnosed problem.
type Issue struct {
	Severity   string `json:"severity"`
	Service    string `json:"service"`
	Message    string `json:"message"`
	Details    string `json:"details,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Result contains the diagnosis results.
type Result struct {
	Issues     []Issue  `json:"issues"`
	Healthy    []string `json:"healthy"`
	NotRunning []string `json:"not_running"`
}

// Diagnose runs diagnostics on all services.
func Diagnose(ctx context.Context, cfg *config.Config, docker runtime.ContainerRuntime) (*Result, error) {
	result := &Result{
		Issues:     []Issue{},
		Healthy:    []string{},
		NotRunning: []string{},
	}

	// Get running containers for this project
	containers, err := docker.ListContainers(ctx, map[string]string{
		"cbox.project": cfg.Project.Name,
	}, true) // Include all containers (running and stopped)

	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Build map of container name -> container
	containerMap := make(map[string]runtime.Container)
	for _, c := range containers {
		serviceName := c.Name
		prefix := cfg.Project.Name + "_"
		if len(serviceName) > len(prefix) && serviceName[:len(prefix)] == prefix {
			serviceName = serviceName[len(prefix):]
		}
		containerMap[serviceName] = c
	}

	// Check each service
	for name, svc := range cfg.Services {
		container, exists := containerMap[name]
		if !exists {
			result.NotRunning = append(result.NotRunning, name)
			continue
		}

		// Check if container is running
		if !strings.Contains(strings.ToLower(container.Status), "up") {
			result.NotRunning = append(result.NotRunning, name)

			// Check if container exited
			if strings.Contains(strings.ToLower(container.Status), "exited") {
				exitCode := extractExitCode(container.Status)
				if exitCode != 0 {
					result.Issues = append(result.Issues, Issue{
						Severity:   SeverityError,
						Service:    name,
						Message:    fmt.Sprintf("Container exited with code %d", exitCode),
						Details:    getLastLogs(ctx, container.Name),
						Suggestion: "Check the logs with 'cbox logs " + name + "' for more details",
					})
				}
			}
			continue
		}

		// Container is running - run checks
		issues := runChecks(ctx, cfg, docker, name, svc, container, containerMap)
		if len(issues) == 0 {
			result.Healthy = append(result.Healthy, name)
		} else {
			result.Issues = append(result.Issues, issues...)
		}
	}

	return result, nil
}

// runChecks runs all diagnostic checks for a service.
func runChecks(ctx context.Context, cfg *config.Config, docker runtime.ContainerRuntime, name string, svc config.Service, container runtime.Container, serviceContainers map[string]runtime.Container) []Issue {
	var issues []Issue

	// Check 1: Restart count (crash loop detection)
	restartCount := getRestartCount(ctx, container.Name)
	if restartCount >= 3 {
		lastLogs := getLastLogs(ctx, container.Name)
		suggestion := "Check logs for crash reason"
		if strings.Contains(lastLogs, "ECONNREFUSED") {
			suggestion = "A service dependency might not be ready. Add healthchecks and depends_on."
		}
		issues = append(issues, Issue{
			Severity:   SeverityError,
			Service:    name,
			Message:    fmt.Sprintf("Container is crash-looping (%d restarts)", restartCount),
			Details:    lastLogs,
			Suggestion: suggestion,
		})
	}

	// Check 2: Health status
	healthStatus := getHealthStatus(ctx, container.Name)
	if healthStatus == "unhealthy" {
		issues = append(issues, Issue{
			Severity:   SeverityError,
			Service:    name,
			Message:    "Container is unhealthy",
			Details:    getLastHealthLog(ctx, container.Name),
			Suggestion: "Check healthcheck configuration and container logs",
		})
	}

	// Check 3: Memory usage
	memUsage, memLimit := getMemoryUsage(ctx, container.Name)
	if memLimit > 0 && memUsage > 0 {
		memPercent := float64(memUsage) / float64(memLimit) * 100
		if memPercent > 80 {
			issues = append(issues, Issue{
				Severity:   SeverityWarning,
				Service:    name,
				Message:    fmt.Sprintf("High memory usage (%.1f%% of limit)", memPercent),
				Suggestion: "Consider increasing memory limit or optimizing the application",
			})
		}
	}

	// Check 4: Port availability (if service has ports)
	if svc.Port > 0 {
		// Check if port mapping changed (remapped due to conflict)
		for _, p := range container.Ports {
			if strings.Contains(p, fmt.Sprintf("->%d", svc.Port)) {
				// Port is mapped, extract host port
				parts := strings.Split(p, "->")
				if len(parts) == 2 {
					hostPart := strings.TrimSpace(parts[0])
					// Extract port number
					colonIdx := strings.LastIndex(hostPart, ":")
					if colonIdx >= 0 {
						hostPortStr := hostPart[colonIdx+1:]
						hostPort, _ := strconv.Atoi(hostPortStr)
						if hostPort != svc.Port && hostPort > 0 {
							pid, _ := runtime.FindProcessOnPort(svc.Port)
							details := fmt.Sprintf("Port %d was in use, remapped to %d", svc.Port, hostPort)
							if pid > 0 {
								details = fmt.Sprintf("Port %d was in use by PID %d, remapped to %d", svc.Port, pid, hostPort)
							}
							issues = append(issues, Issue{
								Severity:   SeverityWarning,
								Service:    name,
								Message:    "Port was remapped due to conflict",
								Details:    details,
								Suggestion: "Free up the original port or update your configuration",
							})
							break // Only report once per service (IPv4 and IPv6 both match)
						}
					}
				}
			}
		}
	}

	// Check 5: Missing dependencies
	for _, dep := range svc.DependsOn {
		depContainer, exists := serviceContainers[dep]
		if !exists {
			issues = append(issues, Issue{
				Severity:   SeverityError,
				Service:    name,
				Message:    fmt.Sprintf("Depends on '%s' which is not running", dep),
				Suggestion: "Start the dependency with 'cbox up " + dep + "'",
			})
		} else if !isContainerRunning(ctx, depContainer.Name) {
			issues = append(issues, Issue{
				Severity:   SeverityError,
				Service:    name,
				Message:    fmt.Sprintf("Depends on '%s' which is not running", dep),
				Suggestion: "Start the dependency with 'cbox up " + dep + "'",
			})
		}
	}

	// Check 6: Connection refused errors in logs
	lastLogs := getLastLogs(ctx, container.Name)
	if strings.Contains(lastLogs, "ECONNREFUSED") || strings.Contains(lastLogs, "Connection refused") {
		issues = append(issues, Issue{
			Severity:   SeverityWarning,
			Service:    name,
			Message:    "Connection refused errors in logs",
			Details:    extractConnectionErrors(lastLogs),
			Suggestion: "Check if dependent services are running and healthy",
		})
	}

	return issues
}

// Helper functions

func getRestartCount(ctx context.Context, containerName string) int {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.RestartCount}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	count, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	return count
}

func getHealthStatus(ctx context.Context, containerName string) string {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Health.Status}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return "none"
	}
	return strings.TrimSpace(string(output))
}

func getLastHealthLog(ctx context.Context, containerName string) string {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{range .State.Health.Log}}{{.Output}}{{end}}", containerName)
	output, _ := cmd.Output()
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return ""
}

func getMemoryUsage(ctx context.Context, containerName string) (int64, int64) {
	// Get memory usage
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{.MemUsage}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0
	}

	// Parse output like "100MiB / 500MiB"
	parts := strings.Split(strings.TrimSpace(string(output)), "/")
	if len(parts) != 2 {
		return 0, 0
	}

	usage := parseMemory(strings.TrimSpace(parts[0]))
	limit := parseMemory(strings.TrimSpace(parts[1]))
	return usage, limit
}

func parseMemory(s string) int64 {
	s = strings.ToUpper(s)
	var multiplier int64 = 1

	if strings.HasSuffix(s, "GIB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GIB")
	} else if strings.HasSuffix(s, "MIB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MIB")
	} else if strings.HasSuffix(s, "KIB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KIB")
	} else if strings.HasSuffix(s, "GB") {
		multiplier = 1000 * 1000 * 1000
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1000 * 1000
		s = strings.TrimSuffix(s, "MB")
	} else if strings.HasSuffix(s, "KB") {
		multiplier = 1000
		s = strings.TrimSuffix(s, "KB")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(val * float64(multiplier))
}

func getLastLogs(ctx context.Context, containerName string) string {
	cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", "10", containerName)
	output, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(output))
}

func extractExitCode(status string) int {
	// Parse "Exited (1) 5 minutes ago"
	if strings.Contains(status, "Exited") {
		start := strings.Index(status, "(")
		end := strings.Index(status, ")")
		if start >= 0 && end > start {
			code, _ := strconv.Atoi(status[start+1 : end])
			return code
		}
	}
	return 0
}

func isContainerRunning(ctx context.Context, containerName string) bool {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

func extractConnectionErrors(logs string) string {
	lines := strings.Split(logs, "\n")
	var errLines []string
	for _, line := range lines {
		if strings.Contains(line, "ECONNREFUSED") || strings.Contains(line, "Connection refused") {
			errLines = append(errLines, strings.TrimSpace(line))
		}
	}
	if len(errLines) > 3 {
		errLines = errLines[len(errLines)-3:]
	}
	return strings.Join(errLines, "\n")
}
