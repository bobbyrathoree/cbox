// Package builder handles Docker image building.
package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	Verbose     bool // Show full Docker output
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
	// Check if path exists before attempting detection
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		return "", fmt.Errorf("service path does not exist: %s", servicePath)
	}

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
		return "", fmt.Errorf("unknown runtime: %s (supported: nodejs, python, go)", runtime)
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

	// Use plain progress for cleaner output (unless verbose)
	if !opts.Verbose {
		args = append(args, "--progress=plain")
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

	// Capture output when not verbose, show only on error
	var buildOutput bytes.Buffer
	if opts.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = &cacheWarningFilter{out: &buildOutput}
	}

	err := cmd.Run()
	if err != nil && !opts.Verbose {
		// Show captured output on build failure
		os.Stderr.Write(buildOutput.Bytes())
	}

	return err
}

// cacheWarningFilter filters out Docker buildx cache warnings from stderr.
// It buffers input to handle warnings that span multiple Write calls.
type cacheWarningFilter struct {
	out    io.Writer
	buffer []byte
}

func (f *cacheWarningFilter) Write(p []byte) (n int, err error) {
	// Append to buffer
	f.buffer = append(f.buffer, p...)

	// Process complete lines
	for {
		idx := bytes.IndexByte(f.buffer, '\n')
		if idx == -1 {
			// No complete line yet, but check if buffer contains warning
			// (some Docker output doesn't end with newline)
			if len(f.buffer) > 500 {
				// Buffer too large, flush it
				if !f.shouldFilter(string(f.buffer)) {
					f.out.Write(f.buffer)
				}
				f.buffer = nil
			}
			break
		}

		line := string(f.buffer[:idx+1])
		f.buffer = f.buffer[idx+1:]

		// Suppress Docker buildx cache warnings on first build
		if !f.shouldFilter(line) {
			f.out.Write([]byte(line))
		}
	}

	return len(p), nil
}

func (f *cacheWarningFilter) shouldFilter(line string) bool {
	// Filter various forms of cache import warnings
	if strings.Contains(line, "local cache import") && strings.Contains(line, "not found") {
		return true
	}
	if strings.Contains(line, "WARNING") && strings.Contains(line, "cache") && strings.Contains(line, "not found") {
		return true
	}
	return false
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
