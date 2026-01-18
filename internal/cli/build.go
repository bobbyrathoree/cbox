package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrathore/cbox/internal/builder"
	"github.com/bobbyrathore/cbox/internal/builder/golang"
	"github.com/bobbyrathore/cbox/internal/builder/nodejs"
	"github.com/bobbyrathore/cbox/internal/builder/python"
	"github.com/bobbyrathore/cbox/internal/config"
	cboxctx "github.com/bobbyrathore/cbox/internal/context"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

// DetectionResult contains info about auto-detected project
type DetectionResult struct {
	ProjectType string // "Node.js", "Python", "Go"
	Framework   string // "Express", "FastAPI", "Gin", etc.
	Extra       string // Package manager or other info
	Environment string // Applied environment (if any)
}

var (
	buildNoCache  bool
	buildParallel bool
	buildEnv      string
)

var buildCmd = &cobra.Command{
	Use:   "build [service...]",
	Short: "Build service images",
	Long: `Build Docker images for one or more services.

If no services are specified, builds all services defined in cbox.yaml.

Examples:
  cbox build                Build all services
  cbox build api            Build specific service
  cbox build --no-cache     Build without cache
  cbox build --env staging  Build with staging environment`,
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "build without cache")
	buildCmd.Flags().BoolVar(&buildParallel, "parallel", true, "build services in parallel")
	buildCmd.Flags().StringVarP(&buildEnv, "env", "e", "", "environment to use (e.g., staging, production)")
}

func runBuild(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOptions(verbose, quiet)

	// Load configuration with detection info
	cfg, detection, err := loadConfigWithDetection()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			getConfigErrorHint(err),
		)
		return err
	}

	// Show auto-detection feedback
	if detection != nil {
		if detection.ProjectType != "" {
			info := detection.ProjectType
			if detection.Framework != "" {
				info += " (" + detection.Framework + ")"
			}
			console.Dim("→ Auto-detected: %s", info)
		}
		// Show stored environment if applied
		if detection.Environment != "" && buildEnv == "" {
			console.Dim("→ Using environment: %s", detection.Environment)
		}
	}

	// Apply environment overrides if explicitly specified (overrides stored env)
	if buildEnv != "" {
		// Only apply if different from already-applied stored env
		appliedEnv := ""
		if detection != nil {
			appliedEnv = detection.Environment
		}
		if buildEnv != appliedEnv {
			// Need to reload and apply the explicit env
			baseCfg, _ := config.Load(GetConfigFile())
			cfg, err = baseCfg.WithEnvironment(buildEnv)
			if err != nil {
				console.Error("Failed to apply environment %q: %s", buildEnv, err)
				return err
			}
		}
		console.Info("Using environment: %s", buildEnv)
	}

	// Determine which services to build
	servicesToBuild := args
	if len(servicesToBuild) == 0 {
		// Build all services that have a path (not pre-built images)
		for name, svc := range cfg.Services {
			if svc.IsBuildService() {
				servicesToBuild = append(servicesToBuild, name)
			}
		}
	}

	if len(servicesToBuild) == 0 {
		console.Info("No services to build")
		return nil
	}

	// Create builder
	b := builder.New(console)

	// Build services
	if buildParallel && len(servicesToBuild) > 1 {
		return buildParallel_(ctx, b, cfg, servicesToBuild, console)
	}
	return buildSequential(ctx, b, cfg, servicesToBuild, console)
}

func buildSequential(ctx context.Context, b *builder.Builder, cfg *config.Config, services []string, console *output.Console) error {
	for _, name := range services {
		svc, ok := cfg.Services[name]
		if !ok {
			console.Error("Unknown service: %s", name)
			continue
		}

		if svc.IsImageService() {
			console.Info("Skipping %s (pre-built image: %s)", name, svc.Image)
			continue
		}

		spin := output.NewSpinner(fmt.Sprintf("Building %s...", name), quiet)
		spin.Start()

		start := time.Now()
		result, err := b.Build(ctx, builder.BuildOptions{
			ServiceName: name,
			Service:     svc,
			ProjectName: cfg.Project.Name,
			NoCache:     buildNoCache,
			Verbose:     verbose,
		})

		if err != nil {
			spin.Fail(fmt.Sprintf("Failed to build %s: %s", name, err))
			return err
		}

		duration := time.Since(start)
		spin.Success(fmt.Sprintf("Built %s in %.1fs → %s", name, duration.Seconds(), result.ImageName))
	}

	return nil
}

func buildParallel_(ctx context.Context, b *builder.Builder, cfg *config.Config, services []string, console *output.Console) error {
	var wg sync.WaitGroup
	errors := make(chan error, len(services))

	for _, name := range services {
		svc, ok := cfg.Services[name]
		if !ok {
			console.Error("Unknown service: %s", name)
			continue
		}

		if svc.IsImageService() {
			console.Info("Skipping %s (pre-built image: %s)", name, svc.Image)
			continue
		}

		wg.Add(1)
		go func(name string, svc config.Service) {
			defer wg.Done()

			console.Info("Building %s...", name)
			start := time.Now()

			result, err := b.Build(ctx, builder.BuildOptions{
				ServiceName: name,
				Service:     svc,
				ProjectName: cfg.Project.Name,
				NoCache:     buildNoCache,
				Verbose:     verbose,
			})

			if err != nil {
				console.Error("Failed to build %s: %s", name, err)
				errors <- err
				return
			}

			duration := time.Since(start)
			console.Success("Built %s in %.1fs → %s", name, duration.Seconds(), result.ImageName)
		}(name, svc)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		return err // Return first error
	}

	return nil
}

// getConfigErrorHint returns an appropriate hint based on error type
func getConfigErrorHint(err error) string {
	msg := err.Error()

	switch {
	case strings.Contains(msg, "config file not found"):
		return "Run 'cbox init' to create a cbox.yaml file"
	case strings.Contains(msg, "invalid YAML"):
		return "Fix the YAML syntax in cbox.yaml"
	case strings.Contains(msg, "validation failed"):
		return "Fix the configuration errors in cbox.yaml"
	case strings.Contains(msg, "environment variable"):
		return "Set the required environment variables"
	case strings.Contains(msg, "could not auto-detect"):
		return "Run 'cbox init' to create a cbox.yaml file"
	default:
		return "Check your cbox.yaml configuration"
	}
}

func loadConfig() (*config.Config, error) {
	cfg, _, err := loadConfigWithDetection()
	return cfg, err
}

// loadConfigRaw loads the config file WITHOUT applying stored environment.
// Use this for env commands (list, show, switch) that need to work even when
// the stored environment has unset variables.
func loadConfigRaw() (*config.Config, error) {
	configPath := GetConfigFile()

	if !config.Exists(configPath) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	return config.Load(configPath)
}

func loadConfigWithDetection() (*config.Config, *DetectionResult, error) {
	configPath := GetConfigFile()

	// Check if config exists
	if !config.Exists(configPath) {
		// If user explicitly specified --config, don't fall through to zero-config
		if configFile != "cbox.yaml" {
			return nil, nil, fmt.Errorf("config file not found: %s", configPath)
		}

		// Try zero-config mode only for default config
		wd, _ := os.Getwd()
		return tryZeroConfig(wd)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}

	// Check for stored environment context
	detection := &DetectionResult{}
	if storedEnv := cboxctx.GetCurrentEnv(); storedEnv != "" {
		if cfg.HasEnvironment(storedEnv) {
			cfg, err = cfg.WithEnvironment(storedEnv)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply stored environment %q: %w", storedEnv, err)
			}
			detection.Environment = storedEnv
		}
		// If environment doesn't exist in config, silently ignore
		// (user may have switched configs or removed the environment)
	}

	return cfg, detection, nil
}

func tryZeroConfig(projectPath string) (*config.Config, *DetectionResult, error) {
	cfg := config.DefaultConfig(projectPath)

	// Check for Node.js project
	if nodejs.IsProject(projectPath) {
		project, _ := nodejs.Detect(projectPath)
		cfg.Services["app"] = config.DefaultNodeService("app", ".", 3000)
		return cfg, &DetectionResult{
			ProjectType: "Node.js",
			Framework:   string(project.Framework),
			Extra:       string(project.PackageManager),
		}, nil
	}

	// Check for Go project
	if golang.IsProject(projectPath) {
		project, _ := golang.Detect(projectPath)
		cfg.Services["app"] = config.Service{
			Path:    ".",
			Runtime: "go",
			Port:    8080,
			Command: []string{"./app"},
		}
		return cfg, &DetectionResult{
			ProjectType: "Go",
			Framework:   string(project.Framework),
		}, nil
	}

	// Check for Python project
	if python.IsProject(projectPath) {
		project, _ := python.Detect(projectPath)
		cfg.Services["app"] = config.Service{
			Path:    ".",
			Runtime: "python",
			Port:    8000,
		}
		return cfg, &DetectionResult{
			ProjectType: "Python",
			Framework:   string(project.Framework),
			Extra:       string(project.PackageManager),
		}, nil
	}

	return nil, nil, fmt.Errorf("no cbox.yaml found and could not auto-detect project type\nSupported: Node.js (package.json), Go (go.mod), Python (requirements.txt)")
}
