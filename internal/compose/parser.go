// Package compose handles docker-compose.yaml parsing and conversion.
package compose

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComposeFile represents a docker-compose.yaml file (v3.x).
type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
	Volumes  map[string]interface{}    `yaml:"volumes"`
	Networks map[string]interface{}    `yaml:"networks"`
}

// ComposeService represents a service in docker-compose.
type ComposeService struct {
	Image       string            `yaml:"image"`
	Build       interface{}       `yaml:"build"` // Can be string or BuildConfig
	Ports       []string          `yaml:"ports"`
	Environment interface{}       `yaml:"environment"` // Can be map or list
	DependsOn   interface{}       `yaml:"depends_on"`  // Can be list or map
	Volumes     []string          `yaml:"volumes"`
	Command     interface{}       `yaml:"command"` // Can be string or list
	Restart     string            `yaml:"restart"`
	Healthcheck *ComposeHealth    `yaml:"healthcheck"`
	Labels      map[string]string `yaml:"labels"`
}

// ComposeHealth represents a healthcheck configuration.
type ComposeHealth struct {
	Test        interface{} `yaml:"test"` // Can be string or list
	Interval    string      `yaml:"interval"`
	Timeout     string      `yaml:"timeout"`
	Retries     int         `yaml:"retries"`
	StartPeriod string      `yaml:"start_period"`
}

// BuildConfig represents build configuration when it's an object.
type BuildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// Parse reads and parses a docker-compose.yaml file.
func Parse(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	return &compose, nil
}

// GetBuildContext extracts the build context from the Build field.
func (s *ComposeService) GetBuildContext() string {
	if s.Build == nil {
		return ""
	}

	switch b := s.Build.(type) {
	case string:
		return b
	case map[string]interface{}:
		if ctx, ok := b["context"].(string); ok {
			return ctx
		}
	}
	return ""
}

// GetEnvironment converts environment to a map.
func (s *ComposeService) GetEnvironment() map[string]string {
	env := make(map[string]string)

	if s.Environment == nil {
		return env
	}

	switch e := s.Environment.(type) {
	case map[string]interface{}:
		for k, v := range e {
			if v == nil {
				env[k] = ""
			} else {
				env[k] = fmt.Sprintf("%v", v)
			}
		}
	case []interface{}:
		// Format: ["KEY=value", "KEY2=value2"]
		for _, item := range e {
			if str, ok := item.(string); ok {
				parts := strings.SplitN(str, "=", 2)
				if len(parts) == 2 {
					env[parts[0]] = parts[1]
				} else if len(parts) == 1 {
					env[parts[0]] = ""
				}
			}
		}
	}

	return env
}

// GetDependsOn extracts dependencies as a string slice.
func (s *ComposeService) GetDependsOn() []string {
	if s.DependsOn == nil {
		return nil
	}

	var deps []string
	switch d := s.DependsOn.(type) {
	case []interface{}:
		for _, item := range d {
			if str, ok := item.(string); ok {
				deps = append(deps, str)
			}
		}
	case map[string]interface{}:
		// Long form: depends_on: { db: { condition: service_healthy } }
		for name := range d {
			deps = append(deps, name)
		}
	}

	return deps
}

// GetCommand extracts command as a string slice.
func (s *ComposeService) GetCommand() []string {
	if s.Command == nil {
		return nil
	}

	switch c := s.Command.(type) {
	case string:
		// Shell-style command - split on spaces (simple approach)
		return strings.Fields(c)
	case []interface{}:
		var cmd []string
		for _, item := range c {
			if str, ok := item.(string); ok {
				cmd = append(cmd, str)
			}
		}
		return cmd
	}

	return nil
}

// GetPort extracts the first host port from ports list.
func (s *ComposeService) GetPort() int {
	if len(s.Ports) == 0 {
		return 0
	}

	// Parse first port mapping
	// Formats: "8080", "8080:80", "127.0.0.1:8080:80"
	portStr := s.Ports[0]

	// Remove protocol if present (e.g., "8080:80/tcp")
	portStr = strings.Split(portStr, "/")[0]

	parts := strings.Split(portStr, ":")
	if len(parts) == 1 {
		// Just port number
		port, _ := strconv.Atoi(parts[0])
		return port
	}

	// Host:Container or IP:Host:Container
	var hostPort string
	if len(parts) == 2 {
		hostPort = parts[0]
	} else if len(parts) == 3 {
		hostPort = parts[1]
	}

	port, _ := strconv.Atoi(hostPort)
	return port
}

// GetContainerPort extracts the container port from first port mapping.
func (s *ComposeService) GetContainerPort() int {
	if len(s.Ports) == 0 {
		return 0
	}

	portStr := s.Ports[0]
	portStr = strings.Split(portStr, "/")[0]

	parts := strings.Split(portStr, ":")
	if len(parts) == 1 {
		port, _ := strconv.Atoi(parts[0])
		return port
	}

	// Last part is always container port
	port, _ := strconv.Atoi(parts[len(parts)-1])
	return port
}
