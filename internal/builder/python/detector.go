// Package python provides Python project detection and Dockerfile generation.
package python

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageManager represents a Python package manager.
type PackageManager string

const (
	Pip    PackageManager = "pip"
	Poetry PackageManager = "poetry"
	Pipenv PackageManager = "pipenv"
	UV     PackageManager = "uv"
)

// Framework represents a detected Python framework.
type Framework string

const (
	FrameworkNone      Framework = ""
	FrameworkFastAPI   Framework = "FastAPI"
	FrameworkFlask     Framework = "Flask"
	FrameworkDjango    Framework = "Django"
	FrameworkStarlette Framework = "Starlette"
)

// Project contains detected Python project information.
type Project struct {
	// Package manager
	PackageManager PackageManager

	// Python version
	PythonVersion string

	// Project type
	Framework Framework

	// Entry points
	EntryPoint string // main.py, app.py, etc.
	AppModule  string // For uvicorn: "main:app"

	// Config files
	HasPyproject    bool
	HasRequirements bool
	HasSetupPy      bool

	// Dependencies (for reference)
	Dependencies []string
}

// Detect analyzes a directory and returns Python project information.
func Detect(projectPath string) (*Project, error) {
	project := &Project{
		PackageManager: Pip,
		PythonVersion:  "3.12",
	}

	// Check config files
	project.HasPyproject = fileExists(filepath.Join(projectPath, "pyproject.toml"))
	project.HasRequirements = fileExists(filepath.Join(projectPath, "requirements.txt"))
	project.HasSetupPy = fileExists(filepath.Join(projectPath, "setup.py"))

	// Detect package manager from lockfiles
	project.PackageManager = detectPackageManager(projectPath)

	// Detect Python version
	project.PythonVersion = detectPythonVersion(projectPath)

	// Detect dependencies and framework
	project.Dependencies = detectDependencies(projectPath, project)
	project.Framework = detectFramework(project.Dependencies)

	// Detect entry point
	project.EntryPoint = detectEntryPoint(projectPath)

	// Determine app module for uvicorn/gunicorn
	project.AppModule = detectAppModule(projectPath, project)

	return project, nil
}

// detectPackageManager determines which package manager to use.
func detectPackageManager(projectPath string) PackageManager {
	// Check in order of preference
	if fileExists(filepath.Join(projectPath, "uv.lock")) {
		return UV
	}
	if fileExists(filepath.Join(projectPath, "poetry.lock")) {
		return Poetry
	}
	if fileExists(filepath.Join(projectPath, "Pipfile.lock")) {
		return Pipenv
	}
	return Pip
}

// detectPythonVersion finds the required Python version.
func detectPythonVersion(projectPath string) string {
	// Check .python-version
	if data, err := os.ReadFile(filepath.Join(projectPath, ".python-version")); err == nil {
		version := strings.TrimSpace(string(data))
		return normalizePythonVersion(version)
	}

	// Check pyproject.toml for requires-python
	if data, err := os.ReadFile(filepath.Join(projectPath, "pyproject.toml")); err == nil {
		content := string(data)
		// Simple regex to find requires-python = ">=3.11" or similar
		re := regexp.MustCompile(`requires-python\s*=\s*["']([^"']+)["']`)
		if matches := re.FindStringSubmatch(content); len(matches) > 1 {
			return extractPythonVersion(matches[1])
		}
	}

	// Check runtime.txt (Heroku style)
	if data, err := os.ReadFile(filepath.Join(projectPath, "runtime.txt")); err == nil {
		content := strings.TrimSpace(string(data))
		if strings.HasPrefix(content, "python-") {
			version := strings.TrimPrefix(content, "python-")
			return normalizePythonVersion(version)
		}
	}

	return "3.12" // Default to Python 3.12 LTS
}

// normalizePythonVersion converts version strings to major.minor format.
func normalizePythonVersion(version string) string {
	// Remove any leading 'python' or 'Python'
	version = strings.TrimPrefix(strings.ToLower(version), "python")
	version = strings.TrimPrefix(version, "-")
	version = strings.TrimSpace(version)

	// Get major.minor (e.g., "3.11.4" -> "3.11")
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0] + ".12" // Default minor if only major given
	}

	return "3.12"
}

// extractPythonVersion extracts version from a constraint like ">=3.11"
func extractPythonVersion(constraint string) string {
	// Handle common patterns: ">=3.11", "^3.11", "~3.11", "3.11"
	constraint = strings.TrimSpace(constraint)
	constraint = strings.TrimPrefix(constraint, ">=")
	constraint = strings.TrimPrefix(constraint, "^")
	constraint = strings.TrimPrefix(constraint, "~")
	constraint = strings.TrimPrefix(constraint, "==")
	constraint = strings.TrimPrefix(constraint, ">")

	// Remove any upper bound (e.g., ">=3.11,<4")
	if idx := strings.Index(constraint, ","); idx > 0 {
		constraint = constraint[:idx]
	}

	return normalizePythonVersion(constraint)
}

// detectDependencies parses dependencies from requirements.txt or pyproject.toml.
func detectDependencies(projectPath string, project *Project) []string {
	var deps []string

	// Try requirements.txt first (most common)
	if project.HasRequirements {
		deps = append(deps, parseRequirementsTxt(filepath.Join(projectPath, "requirements.txt"))...)
	}

	// Try pyproject.toml
	if project.HasPyproject {
		deps = append(deps, parsePyprojectDeps(filepath.Join(projectPath, "pyproject.toml"))...)
	}

	return deps
}

// parseRequirementsTxt extracts package names from requirements.txt.
func parseRequirementsTxt(path string) []string {
	var deps []string

	file, err := os.Open(path)
	if err != nil {
		return deps
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip -r includes and other flags
		if strings.HasPrefix(line, "-") {
			continue
		}

		// Extract package name (before version specifier)
		pkgName := extractPackageName(line)
		if pkgName != "" {
			deps = append(deps, strings.ToLower(pkgName))
		}
	}

	return deps
}

// extractPackageName gets the package name from a requirements line.
func extractPackageName(line string) string {
	// Handle various formats: pkg, pkg==1.0, pkg>=1.0, pkg[extra], etc.
	for _, sep := range []string{"==", ">=", "<=", "!=", "~=", ">", "<", "[", "@"} {
		if idx := strings.Index(line, sep); idx > 0 {
			return strings.TrimSpace(line[:idx])
		}
	}
	return strings.TrimSpace(line)
}

// parsePyprojectDeps extracts dependencies from pyproject.toml.
func parsePyprojectDeps(path string) []string {
	var deps []string

	data, err := os.ReadFile(path)
	if err != nil {
		return deps
	}

	content := string(data)

	// Simple extraction of dependencies (not full TOML parsing)
	// Look for dependencies = [...] or dependencies = { ... }
	// This is a simplified approach - a full TOML parser would be better

	// Pattern for [project] dependencies = ["pkg1", "pkg2"]
	re := regexp.MustCompile(`dependencies\s*=\s*\[([\s\S]*?)\]`)
	if matches := re.FindStringSubmatch(content); len(matches) > 1 {
		// Extract package names from the list
		listContent := matches[1]
		pkgRe := regexp.MustCompile(`["']([a-zA-Z0-9_-]+)`)
		pkgMatches := pkgRe.FindAllStringSubmatch(listContent, -1)
		for _, m := range pkgMatches {
			if len(m) > 1 {
				deps = append(deps, strings.ToLower(m[1]))
			}
		}
	}

	return deps
}

// detectFramework determines which framework is being used.
func detectFramework(deps []string) Framework {
	depSet := make(map[string]bool)
	for _, d := range deps {
		depSet[strings.ToLower(d)] = true
	}

	// Check in order of specificity
	if depSet["fastapi"] {
		return FrameworkFastAPI
	}
	if depSet["flask"] {
		return FrameworkFlask
	}
	if depSet["django"] {
		return FrameworkDjango
	}
	if depSet["starlette"] {
		return FrameworkStarlette
	}

	return FrameworkNone
}

// detectEntryPoint finds the main entry point.
func detectEntryPoint(projectPath string) string {
	// Common entry points in order of preference
	candidates := []string{
		"main.py",
		"app.py",
		"server.py",
		"run.py",
		"application.py",
		"api.py",
		"src/main.py",
		"src/app.py",
		"app/main.py",
		"app/__init__.py",
	}

	for _, candidate := range candidates {
		if fileExists(filepath.Join(projectPath, candidate)) {
			return candidate
		}
	}

	return "main.py" // Default fallback
}

// detectAppModule determines the ASGI/WSGI app module path.
func detectAppModule(projectPath string, project *Project) string {
	entryPoint := project.EntryPoint

	// Convert file path to module path (e.g., "app/main.py" -> "app.main")
	modulePath := strings.TrimSuffix(entryPoint, ".py")
	modulePath = strings.ReplaceAll(modulePath, "/", ".")
	modulePath = strings.ReplaceAll(modulePath, "\\", ".")

	// Read the entry file to detect the app variable name
	entryPath := filepath.Join(projectPath, entryPoint)
	if data, err := os.ReadFile(entryPath); err == nil {
		content := string(data)

		// Common app variable patterns
		patterns := []struct {
			pattern string
			appVar  string
		}{
			{`FastAPI\(\)`, "app"},
			{`Flask\(__name__\)`, "app"},
			{`Flask\(`, "app"},
			{`application\s*=`, "application"},
			{`app\s*=`, "app"},
		}

		for _, p := range patterns {
			if matched, _ := regexp.MatchString(p.pattern, content); matched {
				return modulePath + ":" + p.appVar
			}
		}
	}

	// Default based on framework
	switch project.Framework {
	case FrameworkFastAPI, FrameworkFlask, FrameworkStarlette:
		return modulePath + ":app"
	case FrameworkDjango:
		// Django uses the project.wsgi:application pattern
		return "config.wsgi:application"
	}

	return modulePath + ":app"
}

// GetInstallCommand returns the install command for the package manager.
func (p *Project) GetInstallCommand() []string {
	switch p.PackageManager {
	case UV:
		return []string{"uv", "pip", "install", "-r", "requirements.txt"}
	case Poetry:
		return []string{"poetry", "install", "--no-interaction", "--no-ansi"}
	case Pipenv:
		return []string{"pipenv", "install", "--deploy"}
	default:
		return []string{"pip", "install", "--no-cache-dir", "-r", "requirements.txt"}
	}
}

// GetInstallProdCommand returns the production install command.
func (p *Project) GetInstallProdCommand() []string {
	switch p.PackageManager {
	case UV:
		return []string{"uv", "pip", "install", "-r", "requirements.txt"}
	case Poetry:
		return []string{"poetry", "install", "--no-interaction", "--no-ansi", "--no-dev"}
	case Pipenv:
		return []string{"pipenv", "install", "--deploy", "--ignore-pipfile"}
	default:
		return []string{"pip", "install", "--no-cache-dir", "-r", "requirements.txt"}
	}
}

// GetStartCommand returns the start command.
func (p *Project) GetStartCommand() []string {
	switch p.Framework {
	case FrameworkFastAPI, FrameworkStarlette:
		return []string{"python", "-m", "uvicorn", p.AppModule, "--host", "0.0.0.0", "--port", "8000"}
	case FrameworkFlask:
		return []string{"python", "-m", "gunicorn", "-b", "0.0.0.0:5000", p.AppModule}
	case FrameworkDjango:
		return []string{"python", "-m", "gunicorn", "-b", "0.0.0.0:8000", p.AppModule}
	default:
		// Generic Python execution
		return []string{"python", p.EntryPoint}
	}
}

// GetDevCommand returns the development command.
func (p *Project) GetDevCommand() []string {
	switch p.Framework {
	case FrameworkFastAPI, FrameworkStarlette:
		return []string{"python", "-m", "uvicorn", p.AppModule, "--host", "0.0.0.0", "--port", "8000", "--reload"}
	case FrameworkFlask:
		return []string{"python", "-m", "flask", "run", "--host", "0.0.0.0", "--reload"}
	case FrameworkDjango:
		return []string{"python", "manage.py", "runserver", "0.0.0.0:8000"}
	default:
		return []string{"python", p.EntryPoint}
	}
}

// GetDefaultPort returns the default port for the framework.
func (p *Project) GetDefaultPort() int {
	switch p.Framework {
	case FrameworkFastAPI, FrameworkStarlette:
		return 8000
	case FrameworkFlask:
		return 5000
	case FrameworkDjango:
		return 8000
	}
	return 8000
}

// IsProject checks if the given directory is a Python project.
func IsProject(projectPath string) bool {
	return fileExists(filepath.Join(projectPath, "requirements.txt")) ||
		fileExists(filepath.Join(projectPath, "pyproject.toml")) ||
		fileExists(filepath.Join(projectPath, "setup.py"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
