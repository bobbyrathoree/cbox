// Package config handles cbox.yaml parsing, validation, and defaults.
package config

import "time"

// Config represents the root cbox.yaml configuration.
type Config struct {
	Version  string             `yaml:"version" validate:"required,eq=1"`
	Project  ProjectConfig      `yaml:"project"`
	Services map[string]Service `yaml:"services" validate:"required,min=1,dive"`
	Volumes  map[string]Volume  `yaml:"volumes,omitempty"`
	Secrets  map[string]Secret  `yaml:"secrets,omitempty"`
}

// ProjectConfig contains project-level settings.
type ProjectConfig struct {
	Name string `yaml:"name"` // Defaults to directory name if not set
}

// Service represents a single service in the stack.
type Service struct {
	// Source - one of Path or Image must be set
	Path  string `yaml:"path,omitempty"`  // Build from source directory
	Image string `yaml:"image,omitempty"` // Use existing image

	// Runtime configuration
	Runtime string `yaml:"runtime,omitempty"` // nodejs, go, python, etc.

	// Build configuration
	Build BuildConfig `yaml:"build,omitempty"`

	// Container configuration
	Port     int      `yaml:"port,omitempty"`    // Primary exposed port
	HostPort int      `yaml:"-"`                 // Runtime-only: alternate host port when port conflict detected
	Expose   []int    `yaml:"expose,omitempty"`  // All exposed ports
	Command  []string `yaml:"command,omitempty"` // Override CMD

	// Development mode
	Dev DevConfig `yaml:"dev,omitempty"`

	// Dependencies and health
	DependsOn   []string          `yaml:"depends_on,omitempty"`
	Healthcheck HealthcheckConfig `yaml:"healthcheck,omitempty"`

	// Lifecycle hooks
	Hooks HooksConfig `yaml:"hooks,omitempty"`

	// Environment
	Env     map[string]string `yaml:"env,omitempty"`
	EnvFile string            `yaml:"env_file,omitempty"`
	Secrets []string          `yaml:"secrets,omitempty"` // References to secrets

	// Storage
	Volumes []string `yaml:"volumes,omitempty"` // volume_name:/path or ./host:/container
}

// HooksConfig defines lifecycle hooks for a service.
type HooksConfig struct {
	PostUp  string `yaml:"post-up,omitempty"`  // Run after container starts and is healthy
	PreDown string `yaml:"pre-down,omitempty"` // Run before container stops
}

// BuildConfig contains build-time configuration.
type BuildConfig struct {
	Dockerfile string            `yaml:"dockerfile,omitempty"` // Custom Dockerfile path (escape hatch)
	Target     string            `yaml:"target,omitempty"`     // Multi-stage build target
	Args       map[string]string `yaml:"args,omitempty"`       // Build arguments
	Context    string            `yaml:"context,omitempty"`    // Build context (defaults to Path)
}

// DevConfig contains development mode settings.
type DevConfig struct {
	Command []string    `yaml:"command,omitempty"` // Override command for dev mode
	Watch   WatchConfig `yaml:"watch,omitempty"`   // File watching configuration
	Sync    bool        `yaml:"sync,omitempty"`    // Enable file sync (bind mount)
}

// WatchConfig specifies which files to watch and ignore.
type WatchConfig struct {
	Paths  []string `yaml:"paths,omitempty"`  // Paths to watch (e.g., ["src/", "package.json"])
	Ignore []string `yaml:"ignore,omitempty"` // Patterns to ignore (e.g., ["node_modules/"])
}

// HealthcheckConfig defines how to check service health.
type HealthcheckConfig struct {
	// HTTP healthcheck
	Path string `yaml:"path,omitempty"` // HTTP path to check (e.g., "/health")

	// TCP healthcheck (if Path is empty, just check port is open)

	// Timing
	Interval time.Duration `yaml:"interval,omitempty"` // Time between checks
	Timeout  time.Duration `yaml:"timeout,omitempty"`  // Timeout for each check
	Retries  int           `yaml:"retries,omitempty"`  // Number of retries before unhealthy

	// Start period - grace period before health checks count
	StartPeriod time.Duration `yaml:"start_period,omitempty"`
}

// Volume represents a named volume.
type Volume struct {
	Driver     string            `yaml:"driver,omitempty"`      // Volume driver (default: local)
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"` // Driver options
	External   bool              `yaml:"external,omitempty"`    // Use existing volume
}

// Secret represents a secret configuration.
type Secret struct {
	// Source - one of Env or File must be set
	Env  string `yaml:"env,omitempty"`  // Environment variable name
	File string `yaml:"file,omitempty"` // Path to secret file
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
