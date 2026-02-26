package cli

import (
	"context"
	"fmt"

	"github.com/bobbyrathore/cbox/internal/deployer"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/registry"
	"github.com/spf13/cobra"
)

var (
	deployTag     string
	deployEnv     string
	deployService string
	deployDryRun  bool
	deployPush    bool
	deployWait    bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy services to cloud infrastructure",
	Long: `Deploy services to cloud infrastructure (AWS ECS).

The deployment target must be configured in cbox.yaml under the 'deploy' section.
Images must be pushed to a registry before deployment.

Examples:
  cbox deploy                      Deploy all services
  cbox deploy --tag v1.0.0         Deploy specific version
  cbox deploy --env production     Deploy with production config
  cbox deploy --dry-run            Preview deployment
  cbox deploy --push               Push images before deploying`,
	RunE: runDeploy,
}

func init() {
	deployCmd.Flags().StringVar(&deployTag, "tag", "latest", "image tag to deploy")
	deployCmd.Flags().StringVarP(&deployEnv, "env", "e", "", "environment to deploy (e.g., staging, production)")
	deployCmd.Flags().StringVarP(&deployService, "service", "s", "", "deploy specific service only")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "show what would be deployed")
	deployCmd.Flags().BoolVar(&deployPush, "push", false, "push images before deploying")
	deployCmd.Flags().BoolVar(&deployWait, "wait", true, "wait for deployment to stabilize")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("deploy", err)
			return err
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	// Apply environment overrides if specified
	if deployEnv != "" {
		cfg, err = cfg.WithEnvironment(deployEnv)
		if err != nil {
			if console.IsJSONMode() {
				console.EmitJSONError("deploy", err)
				return err
			}
			console.Error("Failed to apply environment %q: %s", deployEnv, err)
			return err
		}
		console.Info("Using environment: %s", deployEnv)
	}

	// Check deploy configuration
	if cfg.Deploy.Target == "" {
		deployErr := fmt.Errorf("deploy target not configured")
		if console.IsJSONMode() {
			console.EmitJSONError("deploy", deployErr)
			return deployErr
		}
		console.ErrorWithHint(
			"No deployment target configured",
			"Add a 'deploy' section to your cbox.yaml:\n\n"+
				"deploy:\n"+
				"  target: ecs\n"+
				"  ecs:\n"+
				"    region: us-west-2",
		)
		return deployErr
	}

	// Check registry configuration (needed for image URLs)
	if cfg.Registry.Type == "" {
		regErr := fmt.Errorf("registry not configured")
		if console.IsJSONMode() {
			console.EmitJSONError("deploy", regErr)
			return regErr
		}
		console.ErrorWithHint(
			"No registry configured",
			"Add a 'registry' section to your cbox.yaml to specify where images are stored",
		)
		return regErr
	}

	// Create registry client for image names
	reg, err := registry.New(&cfg.Registry, cfg.Project.Name, console)
	if err != nil {
		console.Error("Failed to create registry client: %s", err)
		return err
	}

	// Push images if requested
	if deployPush {
		console.Header("Pushing images...")
		if err := reg.Authenticate(ctx); err != nil {
			return fmt.Errorf("failed to authenticate with registry: %w", err)
		}

		for name, svc := range cfg.Services {
			if !svc.IsBuildService() {
				continue
			}
			if deployService != "" && name != deployService {
				continue
			}

			repoName := fmt.Sprintf("%s-%s", cfg.Project.Name, name)
			if err := reg.EnsureRepository(ctx, repoName); err != nil {
				return fmt.Errorf("failed to ensure repository: %w", err)
			}

			localImage := fmt.Sprintf("%s_%s", cfg.Project.Name, name)
			_, err := reg.Push(ctx, localImage, deployTag)
			if err != nil {
				return fmt.Errorf("failed to push %s: %w", name, err)
			}
			console.Success("Pushed %s", name)
		}
		console.Newline()
	}

	// Build service deploy configs
	getImageName := func(serviceName string) string {
		return reg.GetFullImageName(cfg.Project.Name, serviceName, deployTag)
	}

	var servicesToDeploy []deployer.ServiceDeployConfig
	allServices := deployer.BuildServiceDeployConfigs(cfg, deployTag, getImageName)

	for _, svc := range allServices {
		if deployService != "" && svc.Name != deployService {
			continue
		}
		servicesToDeploy = append(servicesToDeploy, svc)
	}

	if len(servicesToDeploy) == 0 {
		console.Info("No services to deploy")
		return nil
	}

	// Create deployer
	dep, err := deployer.New(&cfg.Deploy, cfg.Project.Name, console)
	if err != nil {
		console.Error("Failed to create deployer: %s", err)
		return err
	}

	// Dry run - just print what would happen
	if deployDryRun {
		if ecsDep, ok := dep.(*deployer.ECSDeployer); ok {
			ecsDep.PrintDryRun(servicesToDeploy)
		} else {
			console.Header("Would deploy %d service(s):", len(servicesToDeploy))
			for _, svc := range servicesToDeploy {
				console.Info("  - %s: %s", svc.Name, svc.Image)
			}
		}
		return nil
	}

	// Deploy
	console.Header("Deploying to %s...", cfg.Deploy.Target)
	if err := dep.Deploy(ctx, servicesToDeploy); err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("deploy", err)
			return err
		}
		console.Error("Deployment failed: %s", err)
		return err
	}

	// Wait for deployment to stabilize
	if deployWait {
		console.Newline()
		for _, svc := range servicesToDeploy {
			if err := dep.Wait(ctx, svc.Name); err != nil {
				console.Warn("Service %s may not be stable: %s", svc.Name, err)
			}
		}
	}

	if console.IsJSONMode() {
		var deployedNames []string
		for _, svc := range servicesToDeploy {
			deployedNames = append(deployedNames, svc.Name)
		}
		console.EmitJSON("deploy", map[string]interface{}{
			"deployed": deployedNames,
			"target":   cfg.Deploy.Target,
			"tag":      deployTag,
		}, nil)
		return nil
	}

	// Show status
	console.Newline()
	status, err := dep.Status(ctx)
	if err == nil && len(status.Services) > 0 {
		console.Header("Deployment Status:")
		for _, svc := range status.Services {
			console.Info("  %s: %s (running: %d/%d)",
				svc.Name, svc.Status, svc.RunningCount, svc.DesiredCount)
		}
	}

	console.Newline()
	console.Success("Deployment complete!")

	return nil
}
