// Package builder handles Docker image building.
package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bobbyrathore/cbox/internal/builder/golang"
	"github.com/bobbyrathore/cbox/internal/builder/nodejs"
	"github.com/bobbyrathore/cbox/internal/builder/python"
	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
)

// Builder handles building Docker images for services.
type Builder struct {
	console  *output.Console
	cacheDir string
}

// New creates a new Builder.
func New(console *output.Console) *Builder {
	return &Builder{
		console:  console,
		cacheDir: ".cbox/cache",
	}
}

// BuildOptions contains options for building an image.
type BuildOptions struct {
	ServiceName string
	Service     config.Service
	ProjectName string
	NoCache     bool
	DevMode     bool
	Tag         string
}

// BuildResult contains the result of a build.
type BuildResult struct {
	ImageName string
	ImageID   string
	Cached    bool
}

// Build builds a Docker image for a service.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	if opts.Service.Image != "" {
		// Pre-built image, no build needed
		return &BuildResult{
			ImageName: opts.Service.Image,
			Cached:    true,
		}, nil
	}

	servicePath := opts.Service.Path
	if !filepath.IsAbs(servicePath) {
		wd, _ := os.Getwd()
		servicePath = filepath.Join(wd, servicePath)
	}

	// Generate image tag
	imageName := opts.Tag
	if imageName == "" {
		imageName = fmt.Sprintf("%s-%s:latest", opts.ProjectName, opts.ServiceName)
	}

	// Determine runtime and generate Dockerfile
	runtime := opts.Service.Runtime
	if runtime == "" {
		runtime = detectRuntime(servicePath)
	}

	var dockerfile string
	var err error

	// Check for custom Dockerfile
	if opts.Service.Build.Dockerfile != "" {
		dockerfilePath := filepath.Join(servicePath, opts.Service.Build.Dockerfile)
		data, err := os.ReadFile(dockerfilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read Dockerfile: %w", err)
		}
		dockerfile = string(data)
	} else {
		// Generate Dockerfile based on runtime
		dockerfile, err = b.generateDockerfile(servicePath, runtime, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to generate Dockerfile: %w", err)
		}
	}

	// Write Dockerfile to temp location
	tempDockerfile := filepath.Join(servicePath, ".cbox.Dockerfile")
	if err := os.WriteFile(tempDockerfile, []byte(dockerfile), 0644); err != nil {
		return nil, fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	defer os.Remove(tempDockerfile)

	// Generate .dockerignore if it doesn't exist
	dockerignorePath := filepath.Join(servicePath, ".dockerignore")
	if _, err := os.Stat(dockerignorePath); os.IsNotExist(err) {
		var ignore string
		switch runtime {
		case "nodejs", "node":
			if project, _ := nodejs.Detect(servicePath); project != nil {
				ignore = nodejs.GenerateDockerignore(project)
			}
		case "python":
			if project, _ := python.Detect(servicePath); project != nil {
				ignore = python.GenerateDockerignore(project)
			}
		case "go", "golang":
			if project, _ := golang.Detect(servicePath); project != nil {
				ignore = golang.GenerateDockerignore(project)
			}
		}
		if ignore != "" {
			os.WriteFile(dockerignorePath+".cbox", []byte(ignore), 0644)
			defer os.Remove(dockerignorePath + ".cbox")
		}
	}

	// Build the image
	if err := b.runBuild(ctx, servicePath, tempDockerfile, imageName, opts); err != nil {
		return nil, err
	}

	return &BuildResult{
		ImageName: imageName,
		Cached:    false,
	}, nil
}

// generateDockerfile creates a Dockerfile for the given runtime.
func (b *Builder) generateDockerfile(servicePath, runtime string, opts BuildOptions) (string, error) {
	switch runtime {
	case "nodejs", "node":
		project, err := nodejs.Detect(servicePath)
		if err != nil {
			return "", fmt.Errorf("failed to detect Node.js project: %w", err)
		}

		port := opts.Service.Port
		if port == 0 {
			port = project.GetDefaultPort()
		}

		if opts.DevMode {
			return nodejs.GenerateDevDockerfile(project, port)
		}
		return nodejs.GenerateDockerfile(project, port)

	case "go", "golang":
		project, err := golang.Detect(servicePath)
		if err != nil {
			return "", fmt.Errorf("failed to detect Go project: %w", err)
		}

		port := opts.Service.Port
		if port == 0 {
			port = project.GetDefaultPort()
		}

		if opts.DevMode {
			return golang.GenerateDevDockerfile(project, port)
		}
		return golang.GenerateDockerfile(project, port)

	case "python":
		project, err := python.Detect(servicePath)
		if err != nil {
			return "", fmt.Errorf("failed to detect Python project: %w", err)
		}

		port := opts.Service.Port
		if port == 0 {
			port = project.GetDefaultPort()
		}

		if opts.DevMode {
			return python.GenerateDevDockerfile(project, port)
		}
		return python.GenerateDockerfile(project, port)

	default:
		return "", fmt.Errorf("unknown runtime: %s", runtime)
	}
}

// runBuild executes the docker build command.
func (b *Builder) runBuild(ctx context.Context, contextPath, dockerfile, imageName string, opts BuildOptions) error {
	args := []string{
		"buildx", "build",
		"-f", dockerfile,
		"-t", imageName,
		"--load", // Load into local Docker
	}

	// Add cache flags
	cacheDir := filepath.Join(contextPath, b.cacheDir)
	os.MkdirAll(cacheDir, 0755)

	if !opts.NoCache {
		args = append(args,
			"--cache-from", fmt.Sprintf("type=local,src=%s", cacheDir),
			"--cache-to", fmt.Sprintf("type=local,dest=%s,mode=max", cacheDir),
		)
	} else {
		args = append(args, "--no-cache")
	}

	// Add build target if specified
	if opts.Service.Build.Target != "" {
		args = append(args, "--target", opts.Service.Build.Target)
	}

	// Add build args
	for k, v := range opts.Service.Build.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	// Add context path
	args = append(args, contextPath)

	b.console.Debug("Running: docker %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// detectRuntime auto-detects the runtime from the project structure.
func detectRuntime(projectPath string) string {
	// Check for Node.js
	if nodejs.IsProject(projectPath) {
		return "nodejs"
	}

	// Check for Go
	if golang.IsProject(projectPath) {
		return "go"
	}

	// Check for Python
	if python.IsProject(projectPath) {
		return "python"
	}

	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
