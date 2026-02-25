package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrathore/cbox/internal/builder"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/registry"
	"github.com/spf13/cobra"
)

// pushedImage tracks a successfully pushed image with its local and remote names.
type pushedImage struct {
	localName  string
	remoteName string
}

var (
	pushTag   string
	pushAll   bool
	pushBuild bool
)

var pushCmd = &cobra.Command{
	Use:   "push [service...]",
	Short: "Push service images to registry",
	Long: `Push Docker images for one or more services to a container registry.

The registry must be configured in cbox.yaml under the 'registry' section.
Supported registries: ecr (AWS), dockerhub

Examples:
  cbox push                    Push all services with 'latest' tag
  cbox push app                Push specific service
  cbox push --tag v1.0.0       Push with specific tag
  cbox push --build            Build images before pushing`,
	RunE: runPush,
}

func init() {
	pushCmd.Flags().StringVar(&pushTag, "tag", "latest", "image tag to push")
	pushCmd.Flags().BoolVar(&pushAll, "all", false, "push all services")
	pushCmd.Flags().BoolVar(&pushBuild, "build", false, "build images before pushing")
}

func runPush(cmd *cobra.Command, args []string) error {
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

	// Check registry configuration
	if cfg.Registry.Type == "" {
		console.ErrorWithHint(
			"No registry configured",
			"Add a 'registry' section to your cbox.yaml:\n\n"+
				"registry:\n"+
				"  type: ecr\n"+
				"  region: us-west-2",
		)
		return fmt.Errorf("registry not configured")
	}

	// Determine which services to push
	servicesToPush := args
	if len(servicesToPush) == 0 || pushAll {
		// Push all build services
		for name, svc := range cfg.Services {
			if svc.IsBuildService() {
				servicesToPush = append(servicesToPush, name)
			}
		}
	}

	if len(servicesToPush) == 0 {
		console.Info("No services to push")
		return nil
	}

	console.Header("Pushing images to %s...", cfg.Registry.Type)

	// Create registry client
	reg, err := registry.New(&cfg.Registry, cfg.Project.Name, console)
	if err != nil {
		console.Error("Failed to create registry client: %s", err)
		return err
	}

	// Authenticate with registry
	if err := reg.Authenticate(ctx); err != nil {
		console.Error("Failed to authenticate with registry: %s", err)
		return err
	}

	// Build images first if requested
	if pushBuild {
		console.Newline()
		console.Header("Building images...")
		b := builder.New(console)
		for _, name := range servicesToPush {
			svc, ok := cfg.Services[name]
			if !ok {
				console.Error("Unknown service: %s", name)
				continue
			}

			if !svc.IsBuildService() {
				continue
			}

			spin := output.NewSpinner(fmt.Sprintf("Building %s...", name), quiet)
			spin.Start()

			start := time.Now()
			_, err := b.Build(ctx, builder.BuildOptions{
				ServiceName: name,
				Service:     svc,
				ProjectName: cfg.Project.Name,
				NoCache:     false,
			})

			if err != nil {
				spin.Fail(fmt.Sprintf("Failed to build %s: %s", name, err))
				return err
			}

			duration := time.Since(start)
			spin.Success(fmt.Sprintf("Built %s in %.1fs", name, duration.Seconds()))
		}
	}

	// Push each service
	console.Newline()
	var pushedImages []pushedImage
	for _, name := range servicesToPush {
		svc, ok := cfg.Services[name]
		if !ok {
			console.Error("Unknown service: %s", name)
			continue
		}

		if !svc.IsBuildService() {
			console.Info("Skipping %s (pre-built image)", name)
			continue
		}

		// Ensure repository exists
		repoName := fmt.Sprintf("%s-%s", cfg.Project.Name, name)
		if err := reg.EnsureRepository(ctx, repoName); err != nil {
			console.Error("Failed to ensure repository: %s", err)
			return err
		}

		// Push the image
		spin := output.NewSpinner(fmt.Sprintf("Pushing %s...", name), quiet)
		spin.Start()

		localImage := fmt.Sprintf("%s-%s:%s", cfg.Project.Name, name, pushTag)
		fullImage, err := reg.Push(ctx, localImage, pushTag)
		if err != nil {
			spin.Fail(fmt.Sprintf("Failed to push %s: %s", name, err))
			return err
		}

		spin.Success(fmt.Sprintf("Pushed %s", fullImage))
		pushedImages = append(pushedImages, pushedImage{
			localName:  localImage,
			remoteName: fullImage,
		})
	}

	// Print summary section
	if len(pushedImages) > 0 {
		console.Newline()
		for _, img := range pushedImages {
			console.Success("Pushed %s -> %s", img.localName, img.remoteName)
		}

		// Extract registry from first pushed image (format: registry/repo:tag)
		registryURL := ""
		if firstImage := pushedImages[0].remoteName; firstImage != "" {
			// Split by "/" and take all parts except the last (which is repo:tag)
			parts := strings.Split(firstImage, "/")
			if len(parts) > 1 {
				registryURL = strings.Join(parts[:len(parts)-1], "/")
			}
		}

		console.Newline()
		console.Info("Summary:")
		console.Info("  Images pushed: %d", len(pushedImages))
		if registryURL != "" {
			console.Info("  Registry: %s", registryURL)
		}
	} else {
		console.Newline()
		console.Info("No images were pushed")
	}

	return nil
}
