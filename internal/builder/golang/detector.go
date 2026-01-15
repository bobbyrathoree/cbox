// Package golang provides Go project detection and Dockerfile generation.
package golang

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Framework represents a detected Go framework.
type Framework string

const (
	FrameworkNone   Framework = ""
	FrameworkGin    Framework = "gin"
	FrameworkEcho   Framework = "echo"
	FrameworkFiber  Framework = "fiber"
	FrameworkChi    Framework = "chi"
	FrameworkStdlib Framework = "stdlib"
)

// Project contains detected Go project information.
type Project struct {
	// Go version
	GoVersion string

	// Module info
	ModulePath string

	// Framework
	Framework Framework

	// Entry points
	EntryPoint string // main.go, cmd/app/main.go, etc.
	BinaryName string // Output binary name

	// Build info
	HasMakefile  bool
	HasVendor    bool
	CGOEnabled   bool
	BuildTags    []string
	LDFlags      string

	// Dependencies (for reference)
	Dependencies []string
}

// Detect analyzes a directory and returns Go project information.
func Detect(projectPath string) (*Project, error) {
	project := &Project{
		GoVersion:  "1.22",
		CGOEnabled: false,
	}

	// Parse go.mod
	goModPath := filepath.Join(projectPath, "go.mod")
	if data, err := os.ReadFile(goModPath); err == nil {
		project.ModulePath, project.GoVersion = parseGoMod(string(data))
		project.Dependencies = parseGoModDeps(string(data))
	}

	// Check for Makefile
	project.HasMakefile = fileExists(filepath.Join(projectPath, "Makefile"))

	// Check for vendor directory
	project.HasVendor = dirExists(filepath.Join(projectPath, "vendor"))

	// Detect framework
	project.Framework = detectFramework(project.Dependencies)

	// Detect entry point
	project.EntryPoint = detectEntryPoint(projectPath)

	// Determine binary name from module path or directory name
	project.BinaryName = detectBinaryName(projectPath, project.ModulePath)

	return project, nil
}

// parseGoMod extracts module path and Go version from go.mod content.
func parseGoMod(content string) (modulePath, goVersion string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Parse module path
		if strings.HasPrefix(line, "module ") {
			modulePath = strings.TrimPrefix(line, "module ")
			modulePath = strings.TrimSpace(modulePath)
		}

		// Parse go version
		if strings.HasPrefix(line, "go ") {
			goVersion = strings.TrimPrefix(line, "go ")
			goVersion = strings.TrimSpace(goVersion)
		}
	}

	if goVersion == "" {
		goVersion = "1.22"
	}

	return modulePath, goVersion
}

// parseGoModDeps extracts dependencies from go.mod require block.
func parseGoModDeps(content string) []string {
	var deps []string

	// Find require block or single requires
	requireRe := regexp.MustCompile(`require\s*\(([\s\S]*?)\)`)
	singleRequireRe := regexp.MustCompile(`require\s+([^\s]+)`)

	// Multi-line require block
	if matches := requireRe.FindStringSubmatch(content); len(matches) > 1 {
		scanner := bufio.NewScanner(strings.NewReader(matches[1]))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			// Extract module path (first word before version)
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				deps = append(deps, parts[0])
			}
		}
	}

	// Single line requires
	singleMatches := singleRequireRe.FindAllStringSubmatch(content, -1)
	for _, match := range singleMatches {
		if len(match) > 1 {
			deps = append(deps, match[1])
		}
	}

	return deps
}

// detectFramework determines which framework is being used.
func detectFramework(deps []string) Framework {
	depSet := make(map[string]bool)
	for _, d := range deps {
		depSet[d] = true
	}

	// Check in order of popularity
	if depSet["github.com/gin-gonic/gin"] {
		return FrameworkGin
	}
	if depSet["github.com/labstack/echo/v4"] || depSet["github.com/labstack/echo"] {
		return FrameworkEcho
	}
	if depSet["github.com/gofiber/fiber/v2"] || depSet["github.com/gofiber/fiber"] {
		return FrameworkFiber
	}
	if depSet["github.com/go-chi/chi/v5"] || depSet["github.com/go-chi/chi"] {
		return FrameworkChi
	}

	// Check for net/http usage (stdlib)
	// This is detected if no framework is found - we default to stdlib
	return FrameworkStdlib
}

// detectEntryPoint finds the main package entry point.
func detectEntryPoint(projectPath string) string {
	// Check for main.go in root
	if fileExists(filepath.Join(projectPath, "main.go")) {
		if containsMainPackage(filepath.Join(projectPath, "main.go")) {
			return "."
		}
	}

	// Check cmd directory (common Go pattern)
	cmdDir := filepath.Join(projectPath, "cmd")
	if dirExists(cmdDir) {
		entries, err := os.ReadDir(cmdDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					mainPath := filepath.Join(cmdDir, entry.Name(), "main.go")
					if fileExists(mainPath) && containsMainPackage(mainPath) {
						return "./cmd/" + entry.Name()
					}
				}
			}
		}
	}

	// Check for any main.go in common locations
	candidates := []string{
		"cmd/server/main.go",
		"cmd/app/main.go",
		"cmd/api/main.go",
		"server/main.go",
		"app/main.go",
	}

	for _, candidate := range candidates {
		fullPath := filepath.Join(projectPath, candidate)
		if fileExists(fullPath) && containsMainPackage(fullPath) {
			// Return the directory containing main.go
			return "./" + filepath.Dir(candidate)
		}
	}

	return "." // Default to root
}

// containsMainPackage checks if a file declares package main.
func containsMainPackage(filePath string) bool {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	content := string(data)
	return strings.Contains(content, "package main")
}

// detectBinaryName determines the output binary name.
func detectBinaryName(projectPath string, modulePath string) string {
	// Use the last part of the module path
	if modulePath != "" {
		parts := strings.Split(modulePath, "/")
		return parts[len(parts)-1]
	}

	// Use directory name as fallback
	return filepath.Base(projectPath)
}

// GetBuildCommand returns the build command.
func (p *Project) GetBuildCommand() []string {
	cmd := []string{
		"go", "build",
		"-ldflags", "-w -s",
		"-o", "/app/" + p.BinaryName,
	}

	if p.EntryPoint != "" {
		cmd = append(cmd, p.EntryPoint)
	} else {
		cmd = append(cmd, ".")
	}

	return cmd
}

// GetStartCommand returns the start command.
func (p *Project) GetStartCommand() []string {
	return []string{"/" + p.BinaryName}
}

// GetDevCommand returns the development command (with air hot reload).
func (p *Project) GetDevCommand() []string {
	// Use air for hot reloading in dev mode
	return []string{"air", "-c", ".air.toml"}
}

// GetDefaultPort returns the default port for the framework.
func (p *Project) GetDefaultPort() int {
	switch p.Framework {
	case FrameworkFiber:
		return 3000
	case FrameworkGin, FrameworkEcho, FrameworkChi, FrameworkStdlib:
		return 8080
	}
	return 8080
}

// IsProject checks if the given directory is a Go project.
func IsProject(projectPath string) bool {
	return fileExists(filepath.Join(projectPath, "go.mod"))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
