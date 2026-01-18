package compose

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbyrathore/cbox/internal/config"
)

// Convert transforms a ComposeFile into a cbox Config.
func Convert(compose *ComposeFile, projectName string) *config.Config {
	cfg := &config.Config{
		Version: "1",
		Project: config.ProjectConfig{
			Name: projectName,
		},
		Services: make(map[string]config.Service),
		Volumes:  make(map[string]config.Volume),
	}

	// Convert services
	for name, svc := range compose.Services {
		cfg.Services[name] = convertService(svc)
	}

	// Convert volumes (just track names, ignore driver config for simplicity)
	for name := range compose.Volumes {
		cfg.Volumes[name] = config.Volume{}
	}

	return cfg
}

// convertService converts a single ComposeService to a cbox Service.
func convertService(svc ComposeService) config.Service {
	service := config.Service{
		Image:     svc.Image,
		Env:       svc.GetEnvironment(),
		DependsOn: svc.GetDependsOn(),
		Command:   svc.GetCommand(),
		Volumes:   convertVolumes(svc.Volumes),
	}

	// Handle build context
	if buildCtx := svc.GetBuildContext(); buildCtx != "" {
		service.Path = buildCtx
		service.Image = "" // Clear image when building from source
	}

	// Handle ports
	hostPort := svc.GetPort()
	containerPort := svc.GetContainerPort()
	if containerPort > 0 {
		service.Port = containerPort
	} else if hostPort > 0 {
		service.Port = hostPort
	}

	// Additional exposed ports
	for i, portStr := range svc.Ports {
		if i == 0 {
			continue // Already handled primary port
		}
		port := parsePortMapping(portStr)
		if port > 0 && port != service.Port {
			service.Expose = append(service.Expose, port)
		}
	}

	// Detect runtime from image name
	if service.Path != "" {
		service.Runtime = detectRuntime(service.Path, service.Image)
	}

	// Convert healthcheck
	if svc.Healthcheck != nil {
		service.Healthcheck = convertHealthcheck(svc.Healthcheck)
	}

	return service
}

// convertVolumes converts docker-compose volume syntax to cbox format.
func convertVolumes(volumes []string) []string {
	var result []string
	for _, v := range volumes {
		// docker-compose formats:
		// - "volume_name:/path"
		// - "./host/path:/container/path"
		// - "/absolute/path:/container/path"
		// cbox uses the same format, so pass through
		result = append(result, v)
	}
	return result
}

// parsePortMapping extracts host port from a port mapping string.
func parsePortMapping(portStr string) int {
	// Remove protocol
	portStr = strings.Split(portStr, "/")[0]
	parts := strings.Split(portStr, ":")

	if len(parts) == 1 {
		// Just port number
		var port int
		for _, c := range parts[0] {
			if c >= '0' && c <= '9' {
				port = port*10 + int(c-'0')
			} else {
				break
			}
		}
		return port
	}

	// Host:Container or IP:Host:Container - get container port
	var port int
	lastPart := parts[len(parts)-1]
	for _, c := range lastPart {
		if c >= '0' && c <= '9' {
			port = port*10 + int(c-'0')
		} else {
			break
		}
	}
	return port
}

// detectRuntime attempts to detect the runtime from build context or image.
func detectRuntime(path, image string) string {
	lower := strings.ToLower(image + " " + path)

	// Check common patterns
	switch {
	case strings.Contains(lower, "node") || strings.Contains(lower, "npm"):
		return "nodejs"
	case strings.Contains(lower, "python") || strings.Contains(lower, "pip"):
		return "python"
	case strings.Contains(lower, "golang") || strings.Contains(lower, "go"):
		return "go"
	case strings.Contains(lower, "rust") || strings.Contains(lower, "cargo"):
		return "rust"
	case strings.Contains(lower, "java") || strings.Contains(lower, "maven") || strings.Contains(lower, "gradle"):
		return "java"
	case strings.Contains(lower, "ruby"):
		return "ruby"
	case strings.Contains(lower, "php"):
		return "php"
	}

	// Check for common files in the path (would need filesystem access)
	// For now, return empty and let cbox auto-detect
	return ""
}

// convertHealthcheck converts compose healthcheck to cbox format.
func convertHealthcheck(hc *ComposeHealth) config.HealthcheckConfig {
	cfg := config.HealthcheckConfig{
		Retries: hc.Retries,
	}

	// Parse intervals
	if hc.Interval != "" {
		cfg.Interval = parseDuration(hc.Interval)
	}
	if hc.Timeout != "" {
		cfg.Timeout = parseDuration(hc.Timeout)
	}
	if hc.StartPeriod != "" {
		cfg.StartPeriod = parseDuration(hc.StartPeriod)
	}

	// Try to extract health path from test command
	// Common patterns: "curl http://localhost:8080/health" or "wget -q http://localhost/health"
	if hc.Test != nil {
		testCmd := extractTestCommand(hc.Test)
		if path := extractHealthPath(testCmd); path != "" {
			cfg.Path = path
		}
	}

	return cfg
}

// parseDuration parses docker-compose duration format (e.g., "30s", "1m30s").
func parseDuration(s string) time.Duration {
	// Simple implementation - Go's time.ParseDuration handles most formats
	d, _ := time.ParseDuration(s)
	return d
}

// extractTestCommand converts healthcheck test to a string.
func extractTestCommand(test interface{}) string {
	switch t := test.(type) {
	case string:
		return t
	case []interface{}:
		var parts []string
		for _, item := range t {
			if str, ok := item.(string); ok {
				// Skip CMD-SHELL prefix
				if str != "CMD-SHELL" && str != "CMD" {
					parts = append(parts, str)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// extractHealthPath extracts the health check path from a test command.
func extractHealthPath(cmd string) string {
	// Look for URLs in the command
	// Pattern: http://localhost:PORT/path or http://127.0.0.1:PORT/path
	for _, part := range strings.Fields(cmd) {
		if strings.HasPrefix(part, "http://localhost") ||
		   strings.HasPrefix(part, "http://127.0.0.1") {
			// Extract path from URL
			if idx := strings.Index(part[7:], "/"); idx > 0 {
				path := part[7+idx:]
				// Clean up path (remove query params, trailing chars)
				if qIdx := strings.Index(path, "?"); qIdx > 0 {
					path = path[:qIdx]
				}
				if path != "/" {
					return path
				}
			}
		}
	}
	return ""
}

// InferProjectName extracts a project name from a file path.
func InferProjectName(composePath string) string {
	// Use parent directory name
	dir := filepath.Dir(composePath)
	if dir == "." || dir == "/" {
		return "app"
	}
	return filepath.Base(dir)
}
