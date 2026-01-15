package registry

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// DockerHubRegistry implements the Registry interface for Docker Hub.
type DockerHubRegistry struct {
	username    string
	repository  string
	projectName string
	console     *output.Console
}

// NewDockerHub creates a new Docker Hub registry client.
func NewDockerHub(cfg *config.RegistryConfig, projectName string, console *output.Console) (*DockerHubRegistry, error) {
	if cfg.Username == "" {
		return nil, fmt.Errorf("Docker Hub username is required; set 'registry.username' in cbox.yaml")
	}

	repository := cfg.Repository
	if repository == "" {
		repository = cfg.Username // Default to username as repo prefix
	}

	return &DockerHubRegistry{
		username:    cfg.Username,
		repository:  repository,
		projectName: projectName,
		console:     console,
	}, nil
}

// Authenticate authenticates Docker with Docker Hub.
func (r *DockerHubRegistry) Authenticate(ctx context.Context) error {
	// Check if already logged in by attempting to inspect credentials
	cmd := exec.CommandContext(ctx, "docker", "login", "--username", r.username)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try to provide helpful error message
		r.console.Info("Docker Hub authentication required")
		r.console.Info("Run 'docker login' manually or set DOCKER_USERNAME and DOCKER_PASSWORD environment variables")
		return fmt.Errorf("docker login required: %w\n%s", err, string(output))
	}

	r.console.Success("Authenticated with Docker Hub as %s", r.username)
	return nil
}

// Push pushes an image to Docker Hub.
func (r *DockerHubRegistry) Push(ctx context.Context, localImage, tag string) (string, error) {
	// Extract service name from local image (format: projectname-servicename:tag)
	// Remove tag if present
	imagePart := strings.Split(localImage, ":")[0]

	// Use the project name to properly extract service name
	// Expected format: {projectName}-{serviceName}
	serviceName := imagePart
	prefix := r.projectName + "-"
	if strings.HasPrefix(imagePart, prefix) {
		serviceName = imagePart[len(prefix):]
	}

	// Get full Docker Hub image name
	fullImageName := r.GetFullImageName(r.projectName, serviceName, tag)

	// Tag the image
	r.console.Info("Tagging %s as %s", localImage, fullImageName)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", localImage, fullImageName)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to tag image: %w\n%s", err, string(output))
	}

	// Push the image
	r.console.Info("Pushing %s...", fullImageName)
	pushCmd := exec.CommandContext(ctx, "docker", "push", fullImageName)
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to push image: %w\n%s", err, string(output))
	}

	return fullImageName, nil
}

// GetFullImageName returns the full Docker Hub image name for a service.
func (r *DockerHubRegistry) GetFullImageName(projectName, serviceName, tag string) string {
	repoName := fmt.Sprintf("%s-%s", projectName, serviceName)
	return fmt.Sprintf("%s/%s:%s", r.repository, repoName, tag)
}

// EnsureRepository is a no-op for Docker Hub (repos are created automatically on push).
func (r *DockerHubRegistry) EnsureRepository(ctx context.Context, repositoryName string) error {
	// Docker Hub creates repositories automatically on first push
	return nil
}
