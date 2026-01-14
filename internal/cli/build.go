package cli

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bobbyrathore/cbox/internal/builder"
	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var (
	buildNoCache  bool
	buildParallel bool
)

var buildCmd = &cobra.Command{
	Use:   "build [service...]",
	Short: "Build service images",
	Long: `Build Docker images for one or more services.

If no services are specified, builds all services defined in cbox.yaml.

Examples:
  cbox build           Build all services
  cbox build api       Build specific service
  cbox build --no-cache Build without cache`,
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "build without cache")
	buildCmd.Flags().BoolVar(&buildParallel, "parallel", true, "build services in parallel")
}

func runBuild(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOptions(verbose, quiet)

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
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

func loadConfig() (*config.Config, error) {
	configPath := GetConfigFile()

	// Check if config exists
	if !config.Exists(configPath) {
		// Try zero-config mode
		wd, _ := os.Getwd()
		return tryZeroConfig(wd)
	}

	return config.Load(configPath)
}

func tryZeroConfig(projectPath string) (*config.Config, error) {
	// Check for Node.js project
	if _, err := os.Stat(projectPath + "/package.json"); err == nil {
		cfg := config.DefaultConfig(projectPath)
		cfg.Services["app"] = config.DefaultNodeService("app", ".", 3000)
		return cfg, nil
	}

	return nil, fmt.Errorf("no cbox.yaml found and could not auto-detect project type")
}
