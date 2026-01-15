// Package registry handles pushing container images to registries.
package registry

import (
	"context"
	"fmt"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// Registry defines the interface for container image registries.
type Registry interface {
	// Authenticate authenticates with the registry.
	Authenticate(ctx context.Context) error

	// Push pushes an image to the registry.
	Push(ctx context.Context, localImage, tag string) (string, error)

	// GetFullImageName returns the full registry image name for a service.
	GetFullImageName(projectName, serviceName, tag string) string

	// EnsureRepository ensures the repository exists (creates if needed).
	EnsureRepository(ctx context.Context, repositoryName string) error
}

// New creates a new registry based on the configuration.
func New(cfg *config.RegistryConfig, projectName string, console *output.Console) (Registry, error) {
	if cfg == nil || cfg.Type == "" {
		return nil, fmt.Errorf("registry type not configured; add 'registry.type' to cbox.yaml")
	}

	switch cfg.Type {
	case "ecr":
		return NewECR(cfg, projectName, console)
	case "dockerhub":
		return NewDockerHub(cfg, projectName, console)
	default:
		return nil, fmt.Errorf("unsupported registry type: %s (supported: ecr, dockerhub)", cfg.Type)
	}
}
