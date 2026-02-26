package cli

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/spf13/cobra"
)

var cleanAll bool

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove stopped containers and unused resources",
	Long: `Remove stopped containers, dangling images, and unused networks.

By default, only removes resources for this project.
Use --all to also remove project images and volumes.

Examples:
  cbox clean           Clean stopped containers and dangling images
  cbox clean --all     Also remove project images and volumes`,
	RunE: runClean,
}

func init() {
	cleanCmd.Flags().BoolVarP(&cleanAll, "all", "a", false, "also remove project images and volumes")
}

func runClean(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOutputMode(verbose, quiet, outputFormat)

	// Load config to get project name
	cfg, err := loadConfig()
	if err != nil {
		if console.IsJSONMode() {
			console.EmitJSONError("clean", err)
			return err
		}
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	console.Header("Cleaning up %s", cfg.Project.Name)

	var totalFreed int64

	// Remove stopped containers for this project
	ns := GetNamespace()
	containerFilterArgs := []string{"ps", "-a", "-q",
		"--filter", fmt.Sprintf("label=cbox.project=%s", cfg.Project.Name),
		"--filter", "status=exited",
		"--filter", "status=created",
	}
	if ns != "" {
		containerFilterArgs = append(containerFilterArgs, "--filter", fmt.Sprintf("label=cbox.namespace=%s", ns))
	}

	// Get stopped containers
	listCmd := exec.CommandContext(ctx, "docker", containerFilterArgs...)
	containerOutput, _ := listCmd.Output()
	containerIDs := strings.Fields(string(containerOutput))

	if len(containerIDs) > 0 {
		removeArgs := append([]string{"rm", "-v"}, containerIDs...)
		exec.CommandContext(ctx, "docker", removeArgs...).Run()
		console.Success("Removed %d stopped container(s)", len(containerIDs))
	} else {
		console.Dim("No stopped containers to remove")
	}

	// Remove dangling images
	pruneCmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	pruneOutput, _ := pruneCmd.CombinedOutput()
	freed := parseFreedSpace(string(pruneOutput))
	totalFreed += freed
	if freed > 0 {
		console.Success("Removed dangling images (%s freed)", formatBytes(freed))
	} else {
		console.Dim("No dangling images to remove")
	}

	// Remove unused networks for this project
	networkPruneArgs := []string{"network", "prune", "-f",
		"--filter", fmt.Sprintf("label=cbox.project=%s", cfg.Project.Name),
	}
	if ns != "" {
		networkPruneArgs = append(networkPruneArgs, "--filter", fmt.Sprintf("label=cbox.namespace=%s", ns))
	}
	networkCmd := exec.CommandContext(ctx, "docker", networkPruneArgs...)
	networkCmd.Run()

	// Also try to remove the project network specifically
	networkName := fmt.Sprintf("cbox_%s", ProjectPrefix(cfg.Project.Name))
	exec.CommandContext(ctx, "docker", "network", "rm", networkName).Run()

	if cleanAll {
		console.Newline()
		console.Warn("Removing project images and volumes...")

		// Remove project images
		imagePrefix := fmt.Sprintf("%s-", cfg.Project.Name)
		listImagesCmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}} {{.Size}}", "--filter", fmt.Sprintf("reference=%s*", imagePrefix))
		imagesOutput, _ := listImagesCmd.Output()

		imageLines := strings.Split(strings.TrimSpace(string(imagesOutput)), "\n")
		removedImages := 0
		for _, line := range imageLines {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				imageName := parts[0]
				rmCmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageName)
				if rmCmd.Run() == nil {
					removedImages++
				}
			}
		}

		if removedImages > 0 {
			console.Success("Removed %d project image(s)", removedImages)
		} else {
			console.Dim("No project images to remove")
		}

		// Remove project volumes
		volumePrefix := fmt.Sprintf("%s_", ProjectPrefix(cfg.Project.Name))
		listVolumesCmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", fmt.Sprintf("name=%s", volumePrefix))
		volumesOutput, _ := listVolumesCmd.Output()

		volumeNames := strings.Fields(string(volumesOutput))
		if len(volumeNames) > 0 {
			rmVolArgs := append([]string{"volume", "rm", "-f"}, volumeNames...)
			exec.CommandContext(ctx, "docker", rmVolArgs...).Run()
			console.Success("Removed %d project volume(s)", len(volumeNames))
		} else {
			console.Dim("No project volumes to remove")
		}
	}

	if console.IsJSONMode() {
		console.EmitJSON("clean", map[string]interface{}{
			"cleaned":     true,
			"space_freed": totalFreed,
		}, nil)
		return nil
	}

	console.Newline()
	if totalFreed > 0 {
		console.Info("Total space freed: %s", formatBytes(totalFreed))
	}
	console.Success("Cleanup complete")

	return nil
}

// parseFreedSpace extracts bytes freed from docker prune output
func parseFreedSpace(output string) int64 {
	// Docker outputs like "Total reclaimed space: 1.234GB"
	re := regexp.MustCompile(`Total reclaimed space:\s*([0-9.]+)\s*([KMGT]?B)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 3 {
		return 0
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	unit := matches[2]
	switch unit {
	case "KB":
		return int64(value * 1024)
	case "MB":
		return int64(value * 1024 * 1024)
	case "GB":
		return int64(value * 1024 * 1024 * 1024)
	case "TB":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(value)
	}
}

// formatBytes formats bytes into human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
