// Package deployer handles deploying container services to cloud infrastructure.
package deployer

import (
	"context"
	"fmt"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// ServiceDeployConfig contains deployment settings for a single service.
type ServiceDeployConfig struct {
	Name            string
	Image           string
	Port            int
	CPU             int
	Memory          int
	DesiredCount    int
	Env             map[string]string
	HealthCheckPath string
}

// ServiceStatus represents the deployment status of a service.
type ServiceStatus struct {
	Name           string
	Status         string
	DesiredCount   int
	RunningCount   int
	PendingCount   int
	TaskDefinition string
	LastUpdated    string
}

// DeploymentStatus represents the overall deployment status.
type DeploymentStatus struct {
	Services []ServiceStatus
}

// Deployer defines the interface for deployment targets.
type Deployer interface {
	// Deploy deploys or updates services.
	Deploy(ctx context.Context, services []ServiceDeployConfig) error

	// Status returns the current deployment status.
	Status(ctx context.Context) (*DeploymentStatus, error)

	// Wait waits for deployment to stabilize.
	Wait(ctx context.Context, serviceName string) error
}

// New creates a new deployer based on the configuration.
func New(cfg *config.DeployConfig, projectName string, console *output.Console) (Deployer, error) {
	if cfg == nil || cfg.Target == "" {
		return nil, fmt.Errorf("deploy target not configured; add 'deploy.target' to cbox.yaml")
	}

	switch cfg.Target {
	case "ecs":
		return NewECS(&cfg.ECS, projectName, console)
	default:
		return nil, fmt.Errorf("unsupported deploy target: %s (supported: ecs)", cfg.Target)
	}
}

// BuildServiceDeployConfigs creates ServiceDeployConfigs from the config.
func BuildServiceDeployConfigs(cfg *config.Config, imageTag string, getImageName func(string) string) []ServiceDeployConfig {
	var services []ServiceDeployConfig

	for name, svc := range cfg.Services {
		if !svc.IsBuildService() {
			continue
		}

		// Get deploy settings
		cpu := 256
		memory := 512
		desiredCount := 1
		healthCheckPath := "/health"

		if svc.Deploy != nil {
			if svc.Deploy.CPU > 0 {
				cpu = svc.Deploy.CPU
			}
			if svc.Deploy.Memory > 0 {
				memory = svc.Deploy.Memory
			}
			if svc.Deploy.DesiredCount > 0 {
				desiredCount = svc.Deploy.DesiredCount
			}
			if svc.Deploy.HealthCheckPath != "" {
				healthCheckPath = svc.Deploy.HealthCheckPath
			}
		}

		services = append(services, ServiceDeployConfig{
			Name:            name,
			Image:           getImageName(name),
			Port:            svc.Port,
			CPU:             cpu,
			Memory:          memory,
			DesiredCount:    desiredCount,
			Env:             svc.Env,
			HealthCheckPath: healthCheckPath,
		})
	}

	return services
}
