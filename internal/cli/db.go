package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/db"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database utilities",
	Long: `Database utilities for working with database containers.

Subcommands:
  shell     Open an interactive database shell
  snapshot  Create, list, restore, and delete database snapshots

Examples:
  cbox db shell                    Open shell for auto-detected DB service
  cbox db shell postgres           Open shell for specific service
  cbox db snapshot create fresh    Create a snapshot named "fresh"
  cbox db snapshot list            List all snapshots
  cbox db snapshot restore fresh   Restore the "fresh" snapshot`,
}

var dbShellCmd = &cobra.Command{
	Use:   "shell [service]",
	Short: "Open database shell",
	Long: `Open an interactive shell for a database container.

Automatically detects the database type (PostgreSQL, MySQL, MongoDB, Redis)
and opens the appropriate client.

Examples:
  cbox db shell            Open shell for auto-detected DB service
  cbox db shell postgres   Open shell for the postgres service`,
	RunE: runDBShell,
}

var dbSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Manage database snapshots",
	Long: `Create, list, restore, and delete database snapshots.

Snapshots are stored in ~/.cbox/snapshots/<project>/<service>/

Supported databases: PostgreSQL, MySQL, MongoDB

Examples:
  cbox db snapshot create fresh    Create a snapshot named "fresh"
  cbox db snapshot list            List all snapshots
  cbox db snapshot restore fresh   Restore the "fresh" snapshot
  cbox db snapshot delete fresh    Delete the "fresh" snapshot`,
}

var snapshotCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a snapshot",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnapshotCreate,
}

var snapshotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List snapshots",
	RunE:  runSnapshotList,
}

var snapshotRestoreCmd = &cobra.Command{
	Use:   "restore <name>",
	Short: "Restore a snapshot",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnapshotRestore,
}

var snapshotDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a snapshot",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnapshotDelete,
}

var dbServiceFlag string

func init() {
	// Add shell command
	dbCmd.AddCommand(dbShellCmd)

	// Add snapshot command with subcommands
	dbSnapshotCmd.AddCommand(snapshotCreateCmd)
	dbSnapshotCmd.AddCommand(snapshotListCmd)
	dbSnapshotCmd.AddCommand(snapshotRestoreCmd)
	dbSnapshotCmd.AddCommand(snapshotDeleteCmd)
	dbCmd.AddCommand(dbSnapshotCmd)

	// Service flag for snapshot commands
	dbSnapshotCmd.PersistentFlags().StringVarP(&dbServiceFlag, "service", "s", "", "database service name")
}

func runDBShell(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	console := output.NewWithOptions(verbose, quiet)

	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	docker := runtime.New(console)

	// Find database service
	var serviceName string
	var containerName string
	var dbType db.DBType

	if len(args) > 0 {
		// Use specified service
		serviceName = args[0]
		if svc, ok := cfg.Services[serviceName]; ok {
			containerName = fmt.Sprintf("%s_%s", cfg.Project.Name, serviceName)
			dbType = db.DetectDBType(svc.Image)
		} else {
			return fmt.Errorf("service '%s' not found in config", serviceName)
		}
	} else {
		// Auto-detect database service
		for name, svc := range cfg.Services {
			detected := db.DetectDBType(svc.Image)
			if detected != db.Unknown {
				serviceName = name
				containerName = fmt.Sprintf("%s_%s", cfg.Project.Name, name)
				dbType = detected
				break
			}
		}
		if serviceName == "" {
			return fmt.Errorf("no database service found in config")
		}
	}

	// Check if container is running
	containers, err := docker.ListContainers(ctx, map[string]string{
		"cbox.service": serviceName,
		"cbox.project": cfg.Project.Name,
	}, false)
	if err != nil || len(containers) == 0 {
		console.ErrorWithHint(
			fmt.Sprintf("Service '%s' is not running", serviceName),
			fmt.Sprintf("Run 'cbox up %s' first", serviceName),
		)
		return fmt.Errorf("service not running")
	}

	console.Info("Connecting to %s (%s)...", serviceName, dbType)

	// Get shell command
	shellCmd := db.ShellCommand(dbType)

	// Run interactive exec
	execArgs := append([]string{"exec", "-it", containerName}, shellCmd...)
	execCmd := exec.CommandContext(ctx, "docker", execArgs...)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	return execCmd.Run()
}

func runSnapshotCreate(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	console := output.NewWithOptions(verbose, quiet)

	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	docker := runtime.New(console)
	snapshotName := args[0]

	// Find database service
	serviceName, containerName, dbType, err := findDBService(cfg, dbServiceFlag, docker, ctx)
	if err != nil {
		return err
	}

	console.Info("Creating snapshot '%s' from %s (%s)...", snapshotName, serviceName, dbType)

	mgr := db.NewSnapshotManager()
	spin := output.NewSpinner(fmt.Sprintf("Dumping %s...", dbType), false)
	spin.Start()

	if err := mgr.Create(ctx, cfg.Project.Name, serviceName, containerName, snapshotName, dbType); err != nil {
		spin.Fail("Failed to create snapshot")
		return err
	}

	spin.Success("Snapshot created")
	console.Success("Created snapshot '%s' for %s", snapshotName, serviceName)

	return nil
}

func runSnapshotList(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	mgr := db.NewSnapshotManager()

	// Find all database services
	var dbServices []string
	for name, svc := range cfg.Services {
		if db.DetectDBType(svc.Image) != db.Unknown {
			dbServices = append(dbServices, name)
		}
	}

	if len(dbServices) == 0 {
		console.Warn("No database services found in config")
		return nil
	}

	// If specific service requested
	if dbServiceFlag != "" {
		dbServices = []string{dbServiceFlag}
	}

	console.Header("Snapshots for %s", cfg.Project.Name)
	console.Newline()

	foundAny := false
	for _, svcName := range dbServices {
		snapshots, err := mgr.List(cfg.Project.Name, svcName)
		if err != nil {
			continue
		}
		if len(snapshots) == 0 {
			continue
		}

		foundAny = true
		console.Info("Service: %s", svcName)

		headers := []string{"NAME", "TYPE", "SIZE", "CREATED"}
		var rows [][]string

		for _, s := range snapshots {
			rows = append(rows, []string{
				s.Name,
				s.DBType,
				db.FormatSize(s.Size),
				s.Created.Format("2006-01-02 15:04"),
			})
		}

		console.Table(headers, rows)
		console.Newline()
	}

	if !foundAny {
		console.Dim("No snapshots found")
		console.Dim("Create one with: cbox db snapshot create <name>")
	}

	return nil
}

func runSnapshotRestore(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	console := output.NewWithOptions(verbose, quiet)

	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	docker := runtime.New(console)
	snapshotName := args[0]

	// Find database service
	serviceName, containerName, dbType, err := findDBService(cfg, dbServiceFlag, docker, ctx)
	if err != nil {
		return err
	}

	console.Warn("Restoring snapshot '%s' to %s (%s)", snapshotName, serviceName, dbType)
	console.Warn("This will overwrite current data!")

	mgr := db.NewSnapshotManager()
	spin := output.NewSpinner(fmt.Sprintf("Restoring %s...", dbType), false)
	spin.Start()

	if err := mgr.Restore(ctx, cfg.Project.Name, serviceName, containerName, snapshotName, dbType); err != nil {
		spin.Fail("Failed to restore snapshot")
		return err
	}

	spin.Success("Snapshot restored")
	console.Success("Restored snapshot '%s' to %s", snapshotName, serviceName)

	return nil
}

func runSnapshotDelete(cmd *cobra.Command, args []string) error {
	console := output.NewWithOptions(verbose, quiet)

	cfg, err := loadConfig()
	if err != nil {
		console.ErrorWithHint(
			fmt.Sprintf("Failed to load config: %s", err),
			"Run 'cbox init' to create a cbox.yaml file",
		)
		return err
	}

	snapshotName := args[0]
	mgr := db.NewSnapshotManager()

	// Find database services to check for the snapshot
	var deleted bool
	for name, svc := range cfg.Services {
		if db.DetectDBType(svc.Image) == db.Unknown {
			continue
		}
		if dbServiceFlag != "" && name != dbServiceFlag {
			continue
		}

		if err := mgr.Delete(cfg.Project.Name, name, snapshotName); err == nil {
			console.Success("Deleted snapshot '%s' from %s", snapshotName, name)
			deleted = true
		}
	}

	if !deleted {
		return fmt.Errorf("snapshot '%s' not found", snapshotName)
	}

	return nil
}

// findDBService finds the database service to use
func findDBService(cfg *config.Config, serviceFlag string, docker *runtime.Docker, ctx context.Context) (serviceName, containerName string, dbType db.DBType, err error) {
	if serviceFlag != "" {
		// Use specified service
		svc, ok := cfg.Services[serviceFlag]
		if !ok {
			err = fmt.Errorf("service '%s' not found in config", serviceFlag)
			return
		}
		serviceName = serviceFlag
		containerName = fmt.Sprintf("%s_%s", cfg.Project.Name, serviceFlag)
		dbType = db.DetectDBType(svc.Image)
		if dbType == db.Unknown {
			err = fmt.Errorf("service '%s' is not a recognized database", serviceFlag)
			return
		}
	} else {
		// Auto-detect database service
		for name, svc := range cfg.Services {
			detected := db.DetectDBType(svc.Image)
			if detected != db.Unknown && detected != db.Redis {
				serviceName = name
				containerName = fmt.Sprintf("%s_%s", cfg.Project.Name, name)
				dbType = detected
				break
			}
		}
		if serviceName == "" {
			err = fmt.Errorf("no database service found in config")
			return
		}
	}

	// Check if container is running
	containers, listErr := docker.ListContainers(ctx, map[string]string{
		"cbox.service": serviceName,
		"cbox.project": cfg.Project.Name,
	}, false)
	if listErr != nil || len(containers) == 0 {
		err = fmt.Errorf("service '%s' is not running - run 'cbox up %s' first", serviceName, serviceName)
		return
	}

	return
}
