// Package config handles cbox.yaml parsing, validation, and defaults.
package config

import (
	"fmt"
	"time"
)

// Config represents the root cbox.yaml configuration.
type Config struct {
	Version      string                        `yaml:"version" validate:"required,eq=1"`
	Project      ProjectConfig                 `yaml:"project"`
	Services     map[string]Service            `yaml:"services" validate:"required,min=1,dive"`
	Volumes      map[string]Volume             `yaml:"volumes,omitempty"`
	Secrets      map[string]Secret             `yaml:"secrets,omitempty"`
	Registry     RegistryConfig                `yaml:"registry,omitempty"`
	Deploy       DeployConfig                  `yaml:"deploy,omitempty"`
	Environments map[string]EnvironmentConfig  `yaml:"environments,omitempty"`
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

	// Deployment
	Deploy *ServiceDeployConfig `yaml:"deploy,omitempty"` // Per-service deploy settings
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

// RegistryConfig defines where to push container images.
type RegistryConfig struct {
	Type       string `yaml:"type,omitempty"`       // ecr, dockerhub
	Region     string `yaml:"region,omitempty"`     // AWS region (for ECR)
	AccountID  string `yaml:"account_id,omitempty"` // AWS account ID (for ECR, auto-detected if empty)
	Username   string `yaml:"username,omitempty"`   // Docker Hub username
	Repository string `yaml:"repository,omitempty"` // Docker Hub repository prefix
}

// DeployConfig defines deployment target configuration.
type DeployConfig struct {
	Target string    `yaml:"target,omitempty"` // ecs (only AWS for now)
	ECS    ECSConfig `yaml:"ecs,omitempty"`    // ECS-specific configuration
}

// ECSConfig contains AWS ECS/Fargate deployment settings.
type ECSConfig struct {
	Cluster          string   `yaml:"cluster,omitempty"`           // ECS cluster name
	Region           string   `yaml:"region,omitempty"`            // AWS region
	VpcID            string   `yaml:"vpc_id,omitempty"`            // VPC ID (optional, uses default)
	Subnets          []string `yaml:"subnets,omitempty"`           // Subnet IDs (optional, auto-discovers)
	SecurityGroups   []string `yaml:"security_groups,omitempty"`   // Security group IDs (optional)
	AssignPublicIP   bool     `yaml:"assign_public_ip,omitempty"`  // Assign public IP to tasks
	ExecutionRoleARN string   `yaml:"execution_role_arn,omitempty"` // IAM execution role
	TaskRoleARN      string   `yaml:"task_role_arn,omitempty"`     // IAM task role
}

// ServiceDeployConfig contains per-service deployment settings.
type ServiceDeployConfig struct {
	CPU             int    `yaml:"cpu,omitempty"`               // Fargate CPU units (256, 512, 1024, etc.)
	Memory          int    `yaml:"memory,omitempty"`            // Fargate memory MB
	DesiredCount    int    `yaml:"desired_count,omitempty"`     // Number of tasks
	HealthCheckPath string `yaml:"health_check_path,omitempty"` // Path for health checks
}

// EnvironmentConfig represents environment-specific overrides.
type EnvironmentConfig struct {
	Services map[string]ServiceOverrides `yaml:"services,omitempty"`
}

// ServiceOverrides contains per-service environment overrides.
type ServiceOverrides struct {
	Env    map[string]string    `yaml:"env,omitempty"`
	Deploy *ServiceDeployConfig `yaml:"deploy,omitempty"`
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

// WithEnvironment returns a new Config with environment-specific overrides applied.
// Environment variable substitution is performed lazily here (not at config load time)
// so that unset variables in unused environments don't cause failures.
func (c *Config) WithEnvironment(env string) (*Config, error) {
	envConfig, ok := c.Environments[env]
	if !ok {
		return nil, fmt.Errorf("environment %q not defined in configuration", env)
	}

	// Deep copy the config
	merged := c.DeepCopy()

	// Apply service overrides
	for serviceName, overrides := range envConfig.Services {
		svc, ok := merged.Services[serviceName]
		if !ok {
			continue // Skip overrides for non-existent services
		}

		// Merge env vars with lazy substitution
		if len(overrides.Env) > 0 {
			if svc.Env == nil {
				svc.Env = make(map[string]string)
			}
			for k, v := range overrides.Env {
				// Substitute env vars NOW (lazy evaluation)
				expanded, err := ExpandEnvString(v)
				if err != nil {
					return nil, fmt.Errorf("service %q env %q: %w", serviceName, k, err)
				}
				svc.Env[k] = expanded
			}
		}

		// Merge deploy config
		if overrides.Deploy != nil {
			if svc.Deploy == nil {
				svc.Deploy = &ServiceDeployConfig{}
			}
			if overrides.Deploy.CPU > 0 {
				svc.Deploy.CPU = overrides.Deploy.CPU
			}
			if overrides.Deploy.Memory > 0 {
				svc.Deploy.Memory = overrides.Deploy.Memory
			}
			if overrides.Deploy.DesiredCount > 0 {
				svc.Deploy.DesiredCount = overrides.Deploy.DesiredCount
			}
			if overrides.Deploy.HealthCheckPath != "" {
				svc.Deploy.HealthCheckPath = overrides.Deploy.HealthCheckPath
			}
		}

		merged.Services[serviceName] = svc
	}

	return merged, nil
}

// DeepCopy creates a deep copy of the Config.
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}

	copy := &Config{
		Version: c.Version,
		Project: c.Project,
		Registry: c.Registry,
		Deploy:   c.Deploy,
	}

	// Copy services
	if c.Services != nil {
		copy.Services = make(map[string]Service, len(c.Services))
		for k, v := range c.Services {
			svcCopy := v
			if v.Env != nil {
				svcCopy.Env = make(map[string]string, len(v.Env))
				for ek, ev := range v.Env {
					svcCopy.Env[ek] = ev
				}
			}
			if v.Deploy != nil {
				deployCopy := *v.Deploy
				svcCopy.Deploy = &deployCopy
			}
			copy.Services[k] = svcCopy
		}
	}

	// Copy volumes
	if c.Volumes != nil {
		copy.Volumes = make(map[string]Volume, len(c.Volumes))
		for k, v := range c.Volumes {
			copy.Volumes[k] = v
		}
	}

	// Copy secrets
	if c.Secrets != nil {
		copy.Secrets = make(map[string]Secret, len(c.Secrets))
		for k, v := range c.Secrets {
			copy.Secrets[k] = v
		}
	}

	// Copy environments
	if c.Environments != nil {
		copy.Environments = make(map[string]EnvironmentConfig, len(c.Environments))
		for k, v := range c.Environments {
			copy.Environments[k] = v
		}
	}

	return copy
}

// HasEnvironment returns true if the named environment is defined.
func (c *Config) HasEnvironment(env string) bool {
	_, ok := c.Environments[env]
	return ok
}
