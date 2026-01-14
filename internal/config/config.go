// Package config handles cbox.yaml parsing, validation, and defaults.
package config

import "time"

// Config represents the root cbox.yaml configuration.
type Config struct {
	Version  string             `yaml:"version" validate:"required,eq=1"`
	Project  ProjectConfig      `yaml:"project"`
	Services map[string]Service `yaml:"services" validate:"required,min=1,dive"`
	Volumes  map[string]Volume  `yaml:"volumes"`
	Secrets  map[string]Secret  `yaml:"secrets"`
}

// ProjectConfig contains project-level settings.
type ProjectConfig struct {
	Name string `yaml:"name"` // Defaults to directory name if not set
}

// Service represents a single service in the stack.
type Service struct {
	// Source - one of Path or Image must be set
	Path  string `yaml:"path"`  // Build from source directory
	Image string `yaml:"image"` // Use existing image

	// Runtime configuration
	Runtime string `yaml:"runtime"` // nodejs, go, python, etc.

	// Build configuration
	Build BuildConfig `yaml:"build"`

	// Container configuration
	Port    int      `yaml:"port"`    // Primary exposed port
	Expose  []int    `yaml:"expose"`  // All exposed ports
	Command []string `yaml:"command"` // Override CMD

	// Development mode
	Dev DevConfig `yaml:"dev"`

	// Dependencies and health
	DependsOn   []string          `yaml:"depends_on"`
	Healthcheck HealthcheckConfig `yaml:"healthcheck"`

	// Environment
	Env     map[string]string `yaml:"env"`
	EnvFile string            `yaml:"env_file"`
	Secrets []string          `yaml:"secrets"` // References to secrets

	// Storage
	Volumes []string `yaml:"volumes"` // volume_name:/path or ./host:/container
}

// BuildConfig contains build-time configuration.
type BuildConfig struct {
	Dockerfile string            `yaml:"dockerfile"` // Custom Dockerfile path (escape hatch)
	Target     string            `yaml:"target"`     // Multi-stage build target
	Args       map[string]string `yaml:"args"`       // Build arguments
	Context    string            `yaml:"context"`    // Build context (defaults to Path)
}

// DevConfig contains development mode settings.
type DevConfig struct {
	Command []string    `yaml:"command"` // Override command for dev mode
	Watch   WatchConfig `yaml:"watch"`   // File watching configuration
	Sync    bool        `yaml:"sync"`    // Enable file sync (bind mount)
}

// WatchConfig specifies which files to watch and ignore.
type WatchConfig struct {
	Paths  []string `yaml:"paths"`  // Paths to watch (e.g., ["src/", "package.json"])
	Ignore []string `yaml:"ignore"` // Patterns to ignore (e.g., ["node_modules/"])
}

// HealthcheckConfig defines how to check service health.
type HealthcheckConfig struct {
	// HTTP healthcheck
	Path string `yaml:"path"` // HTTP path to check (e.g., "/health")

	// TCP healthcheck (if Path is empty, just check port is open)

	// Timing
	Interval time.Duration `yaml:"interval"` // Time between checks
	Timeout  time.Duration `yaml:"timeout"`  // Timeout for each check
	Retries  int           `yaml:"retries"`  // Number of retries before unhealthy

	// Start period - grace period before health checks count
	StartPeriod time.Duration `yaml:"start_period"`
}

// Volume represents a named volume.
type Volume struct {
	Driver     string            `yaml:"driver"`      // Volume driver (default: local)
	DriverOpts map[string]string `yaml:"driver_opts"` // Driver options
	External   bool              `yaml:"external"`    // Use existing volume
}

// Secret represents a secret configuration.
type Secret struct {
	// Source - one of Env or File must be set
	Env  string `yaml:"env"`  // Environment variable name
	File string `yaml:"file"` // Path to secret file
}

// IsBuildService returns true if this service builds from source.
func (s *Service) IsBuildService() bool {
	return s.Path != ""
}

// IsImageService returns true if this service uses a pre-built image.
func (s *Service) IsImageService() bool {
	return s.Image != ""
}

// GetPrimaryPort returns the primary port or first exposed port.
func (s *Service) GetPrimaryPort() int {
	if s.Port != 0 {
		return s.Port
	}
	if len(s.Expose) > 0 {
		return s.Expose[0]
	}
	return 0
}

// GetAllPorts returns all ports that should be exposed.
func (s *Service) GetAllPorts() []int {
	ports := make(map[int]bool)
	if s.Port != 0 {
		ports[s.Port] = true
	}
	for _, p := range s.Expose {
		ports[p] = true
	}
	result := make([]int, 0, len(ports))
	for p := range ports {
		result = append(result, p)
	}
	return result
}

// HasHealthcheck returns true if a healthcheck is configured.
func (s *Service) HasHealthcheck() bool {
	return s.Healthcheck.Path != "" || s.Port != 0
}
