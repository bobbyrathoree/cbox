// Package nodejs provides Node.js project detection and Dockerfile generation.
package nodejs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PackageManager represents a Node.js package manager.
type PackageManager string

const (
	NPM  PackageManager = "npm"
	Yarn PackageManager = "yarn"
	Pnpm PackageManager = "pnpm"
	Bun  PackageManager = "bun"
)

// Framework represents a detected Node.js framework.
type Framework string

const (
	FrameworkNone    Framework = ""
	FrameworkExpress Framework = "Express"
	FrameworkFastify Framework = "Fastify"
	FrameworkNest    Framework = "NestJS"
	FrameworkNext    Framework = "Next.js"
	FrameworkRemix   Framework = "Remix"
	FrameworkVite    Framework = "Vite"
	FrameworkAstro   Framework = "Astro"
)

// Project contains detected Node.js project information.
type Project struct {
	// Package manager
	PackageManager PackageManager
	YarnBerry      bool // Yarn 2+ (Berry) uses different install commands

	// Project type
	HasTypeScript bool
	IsESM         bool // "type": "module" in package.json
	Framework     Framework

	// Entry points
	MainFile   string // "main" field
	EntryPoint string // Detected entry point

	// Scripts
	BuildScript   string // npm run build
	StartScript   string // npm start
	DevScript     string // npm run dev
	HasBuildStep  bool

	// Dependencies (for reference)
	Dependencies    map[string]string
	DevDependencies map[string]string

	// Output
	OutputDir string // dist, build, .next, etc.

	// Node version (from .nvmrc, .node-version, or engines)
	NodeVersion string
}

// PackageJSON represents a parsed package.json file.
type PackageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Main            string            `json:"main"`
	Type            string            `json:"type"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

// Detect analyzes a directory and returns Node.js project information.
func Detect(projectPath string) (*Project, error) {
	pkgPath := filepath.Join(projectPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil, err
	}

	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	project := &Project{
		Dependencies:    pkg.Dependencies,
		DevDependencies: pkg.DevDependencies,
		MainFile:        pkg.Main,
		IsESM:           pkg.Type == "module",
	}

	// Detect package manager from lockfiles
	project.PackageManager = detectPackageManager(projectPath)
	project.YarnBerry = detectYarnBerry(projectPath)

	// Detect TypeScript
	project.HasTypeScript = detectTypeScript(projectPath, pkg)

	// Detect framework
	project.Framework = detectFramework(pkg)

	// Parse scripts
	project.BuildScript = pkg.Scripts["build"]
	project.StartScript = pkg.Scripts["start"]
	project.DevScript = pkg.Scripts["dev"]
	project.HasBuildStep = project.BuildScript != ""

	// Detect entry point
	project.EntryPoint = detectEntryPoint(projectPath, pkg, project)

	// Detect output directory
	project.OutputDir = detectOutputDir(project)

	// Detect Node version
	project.NodeVersion = detectNodeVersion(projectPath, pkg)

	return project, nil
}

// detectPackageManager determines which package manager to use.
func detectPackageManager(projectPath string) PackageManager {
	lockfiles := map[string]PackageManager{
		"bun.lockb":       Bun,
		"pnpm-lock.yaml":  Pnpm,
		"yarn.lock":       Yarn,
		"package-lock.json": NPM,
	}

	// Check in order of preference
	for lockfile, pm := range lockfiles {
		if _, err := os.Stat(filepath.Join(projectPath, lockfile)); err == nil {
			return pm
		}
	}

	return NPM // Default
}

// detectYarnBerry checks if this is a Yarn 2+ (Berry) project.
func detectYarnBerry(projectPath string) bool {
	yarnrcPath := filepath.Join(projectPath, ".yarnrc.yml")
	if _, err := os.Stat(yarnrcPath); err == nil {
		return true
	}
	return false
}

// detectTypeScript checks for TypeScript usage.
func detectTypeScript(projectPath string, pkg PackageJSON) bool {
	// Check for tsconfig.json
	if _, err := os.Stat(filepath.Join(projectPath, "tsconfig.json")); err == nil {
		return true
	}

	// Check for typescript in dependencies
	if _, ok := pkg.DevDependencies["typescript"]; ok {
		return true
	}
	if _, ok := pkg.Dependencies["typescript"]; ok {
		return true
	}

	return false
}

// detectFramework determines which framework is being used.
func detectFramework(pkg PackageJSON) Framework {
	deps := make(map[string]bool)
	for k := range pkg.Dependencies {
		deps[k] = true
	}
	for k := range pkg.DevDependencies {
		deps[k] = true
	}

	// Check in order of specificity
	if deps["next"] {
		return FrameworkNext
	}
	if deps["@remix-run/node"] || deps["@remix-run/react"] {
		return FrameworkRemix
	}
	if deps["astro"] {
		return FrameworkAstro
	}
	if deps["@nestjs/core"] {
		return FrameworkNest
	}
	if deps["fastify"] {
		return FrameworkFastify
	}
	if deps["express"] {
		return FrameworkExpress
	}
	if deps["vite"] {
		return FrameworkVite
	}

	return FrameworkNone
}

// detectEntryPoint finds the main entry point.
func detectEntryPoint(projectPath string, pkg PackageJSON, project *Project) string {
	// Framework-specific entry points
	switch project.Framework {
	case FrameworkNext:
		return "" // Next.js doesn't need an explicit entry point
	case FrameworkRemix:
		return "" // Remix handles its own entry
	}

	// Check package.json main field
	if pkg.Main != "" {
		return pkg.Main
	}

	// Common entry points in order of preference
	candidates := []string{
		"src/index.ts",
		"src/index.js",
		"src/main.ts",
		"src/main.js",
		"src/app.ts",
		"src/app.js",
		"index.ts",
		"index.js",
		"server.ts",
		"server.js",
		"app.ts",
		"app.js",
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(projectPath, candidate)); err == nil {
			return candidate
		}
	}

	return "index.js" // Default fallback
}

// detectOutputDir determines where built files go.
func detectOutputDir(project *Project) string {
	switch project.Framework {
	case FrameworkNext:
		return ".next"
	case FrameworkRemix:
		return "build"
	case FrameworkVite, FrameworkAstro:
		return "dist"
	}

	// Check build script for hints
	if strings.Contains(project.BuildScript, "tsc") {
		// TypeScript usually outputs to dist or build
		return "dist"
	}

	if project.HasBuildStep {
		return "dist" // Common convention
	}

	return "" // No build output
}

// detectNodeVersion finds the required Node.js version.
func detectNodeVersion(projectPath string, pkg PackageJSON) string {
	// Check .nvmrc
	if data, err := os.ReadFile(filepath.Join(projectPath, ".nvmrc")); err == nil {
		version := strings.TrimSpace(string(data))
		return normalizeNodeVersion(version)
	}

	// Check .node-version
	if data, err := os.ReadFile(filepath.Join(projectPath, ".node-version")); err == nil {
		version := strings.TrimSpace(string(data))
		return normalizeNodeVersion(version)
	}

	// Check package.json engines
	if pkg.Engines.Node != "" {
		return parseEnginesVersion(pkg.Engines.Node)
	}

	return "20" // Default to Node 20 LTS
}

// normalizeNodeVersion converts version strings to major version.
func normalizeNodeVersion(version string) string {
	// Remove 'v' prefix
	version = strings.TrimPrefix(version, "v")

	// Get major version
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		return parts[0]
	}

	return "20"
}

// parseEnginesVersion extracts a usable version from engines constraint.
func parseEnginesVersion(constraint string) string {
	// Handle common patterns: ">=18", "^18", "18.x", "18"
	constraint = strings.TrimSpace(constraint)
	constraint = strings.TrimPrefix(constraint, ">=")
	constraint = strings.TrimPrefix(constraint, "^")
	constraint = strings.TrimPrefix(constraint, "~")
	constraint = strings.TrimPrefix(constraint, "v")

	// Get the major version number
	parts := strings.Split(constraint, ".")
	if len(parts) > 0 {
		// Remove any non-numeric suffix
		major := strings.TrimRight(parts[0], "x*")
		if major != "" {
			return major
		}
	}

	return "20"
}

// GetInstallCommand returns the install command for the package manager.
func (p *Project) GetInstallCommand() []string {
	switch p.PackageManager {
	case Bun:
		return []string{"bun", "install", "--frozen-lockfile"}
	case Pnpm:
		return []string{"pnpm", "install", "--frozen-lockfile"}
	case Yarn:
		if p.YarnBerry {
			return []string{"yarn", "install", "--immutable"}
		}
		return []string{"yarn", "install", "--frozen-lockfile"}
	default:
		return []string{"npm", "ci"}
	}
}

// GetBuildCommand returns the build command.
func (p *Project) GetBuildCommand() []string {
	if !p.HasBuildStep {
		return nil
	}
	switch p.PackageManager {
	case Bun:
		return []string{"bun", "run", "build"}
	case Pnpm:
		return []string{"pnpm", "run", "build"}
	case Yarn:
		return []string{"yarn", "build"}
	default:
		return []string{"npm", "run", "build"}
	}
}

// GetStartCommand returns the start command.
func (p *Project) GetStartCommand() []string {
	// Framework-specific start commands
	switch p.Framework {
	case FrameworkNext:
		return []string{"npm", "start"} // next start
	}

	// If there's a build step, run the built output
	if p.HasBuildStep && p.EntryPoint != "" {
		entry := p.EntryPoint
		if p.HasTypeScript && p.OutputDir != "" {
			// Convert src/index.ts to dist/index.js
			entry = strings.Replace(entry, "src/", p.OutputDir+"/", 1)
			entry = strings.Replace(entry, ".ts", ".js", 1)
		}
		return []string{"node", entry}
	}

	// Fall back to npm start
	if p.StartScript != "" {
		switch p.PackageManager {
		case Bun:
			return []string{"bun", "run", "start"}
		case Pnpm:
			return []string{"pnpm", "run", "start"}
		case Yarn:
			return []string{"yarn", "start"}
		default:
			return []string{"npm", "start"}
		}
	}

	// Direct node execution
	if p.EntryPoint != "" {
		return []string{"node", p.EntryPoint}
	}

	return []string{"npm", "start"}
}

// GetDevCommand returns the development command.
func (p *Project) GetDevCommand() []string {
	if p.DevScript != "" {
		switch p.PackageManager {
		case Bun:
			return []string{"bun", "run", "dev"}
		case Pnpm:
			return []string{"pnpm", "run", "dev"}
		case Yarn:
			return []string{"yarn", "dev"}
		default:
			return []string{"npm", "run", "dev"}
		}
	}

	// Fallback: use nodemon or direct node
	return []string{"npm", "run", "dev"}
}

// GetDefaultPort returns the default port for the framework.
func (p *Project) GetDefaultPort() int {
	switch p.Framework {
	case FrameworkNext:
		return 3000
	case FrameworkVite:
		return 5173
	case FrameworkRemix:
		return 3000
	case FrameworkAstro:
		return 4321
	case FrameworkNest, FrameworkExpress, FrameworkFastify:
		return 3000
	}
	return 3000
}

// GetCacheDirectory returns the npm/yarn/pnpm cache mount target.
func (p *Project) GetCacheDirectory() string {
	switch p.PackageManager {
	case Bun:
		return "/root/.bun/install/cache"
	case Pnpm:
		return "/root/.local/share/pnpm/store"
	case Yarn:
		if p.YarnBerry {
			return "/root/.yarn/berry/cache"
		}
		return "/usr/local/share/.cache/yarn"
	default:
		return "/root/.npm"
	}
}

// GetLockfileName returns the name of the lockfile.
func (p *Project) GetLockfileName() string {
	switch p.PackageManager {
	case Bun:
		return "bun.lockb"
	case Pnpm:
		return "pnpm-lock.yaml"
	case Yarn:
		return "yarn.lock"
	default:
		return "package-lock.json"
	}
}

// IsProject checks if the given directory is a Node.js project.
func IsProject(projectPath string) bool {
	_, err := os.Stat(filepath.Join(projectPath, "package.json"))
	return err == nil
}
