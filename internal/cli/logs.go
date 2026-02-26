package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
	"github.com/spf13/cobra"
)

var (
	logsFollow  bool
	logsTail    int
	logsSince   string
	logsNoColor bool
)

var logsCmd = &cobra.Command{
	Use:   "logs [service...]",
	Short: "View service logs",
	Long: `View logs from one or more services.

If no service is specified, shows multiplexed logs from all services.

Examples:
  cbox logs -f         Follow all logs
  cbox logs api        Show api logs
  cbox logs --tail 50  Show last 50 lines`,
	RunE: runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().IntVar(&logsTail, "tail", 100, "number of lines to show")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "show logs since timestamp")
	logsCmd.Flags().BoolVar(&logsNoColor, "no-color", false, "disable colored output")
}

func runLogs(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	docker := runtime.New(console)

	// Get namespace for container name resolution
	ns := GetNamespace()
	projectPrefix := cfg.Project.Name
	if ns != "" {
		projectPrefix = fmt.Sprintf("%s-%s", ns, cfg.Project.Name)
	}

	// Validate service names if provided
	if len(args) > 0 {
		for _, arg := range args {
			if _, ok := cfg.Services[arg]; !ok {
				available := make([]string, 0, len(cfg.Services))
				for name := range cfg.Services {
					available = append(available, name)
				}
				sort.Strings(available)
				return fmt.Errorf("unknown service '%s' (available: %s)", arg, strings.Join(available, ", "))
			}
		}
	}

	// Determine which services to show logs from
	services := args
	if len(services) == 0 {
		for name := range cfg.Services {
			services = append(services, name)
		}
	}

	if len(services) == 1 {
		// Single service - stream directly
		containerName := fmt.Sprintf("%s_%s", projectPrefix, services[0])
		return streamLogs(ctx, docker, containerName, services[0], console, logsFollow, logsTail)
	}

	// Multiple services - multiplex logs
	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		go func(serviceName string) {
			defer wg.Done()
			containerName := fmt.Sprintf("%s_%s", projectPrefix, serviceName)
			streamLogs(ctx, docker, containerName, serviceName, console, logsFollow, logsTail)
		}(svc)
	}

	wg.Wait()
	return nil
}

func streamLogs(ctx context.Context, docker runtime.ContainerRuntime, containerName, serviceName string, console *output.Console, follow bool, tail int) error {
	reader, err := docker.ContainerLogs(ctx, containerName, follow, tail)
	if err != nil {
		console.Error("Failed to get logs for %s: %s", serviceName, err)
		return err
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
			line := scanner.Text()
			console.ServiceLog(serviceName, line)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}

	return nil
}
