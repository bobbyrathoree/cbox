// Package runtime handles container lifecycle management.
package runtime

import (
	"context"
	"io"
	"time"
)

// ContainerRuntime defines the interface for container lifecycle management.
// This allows mocking the Docker runtime for testing.
type ContainerRuntime interface {
	CreateNetwork(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
	CreateContainer(ctx context.Context, cfg ContainerConfig) (string, error)
	StartContainer(ctx context.Context, nameOrID string) error
	StopContainer(ctx context.Context, nameOrID string, timeout time.Duration) error
	RestartContainer(ctx context.Context, nameOrID string, timeout time.Duration) error
	RemoveContainer(ctx context.Context, nameOrID string) error
	ContainerExists(ctx context.Context, nameOrID string) bool
	IsContainerRunning(ctx context.Context, nameOrID string) bool
	ListContainers(ctx context.Context, labels map[string]string, all bool) ([]Container, error)
	GetContainerStats(ctx context.Context, labels map[string]string) (map[string]ContainerStats, error)
	ContainerLogs(ctx context.Context, nameOrID string, follow bool, tail int) (io.ReadCloser, error)
	ContainerExec(ctx context.Context, nameOrID string, cmd []string, interactive, tty bool) error
	ContainerExecWithOutput(ctx context.Context, nameOrID string, command string) (string, error)
	WaitHealthy(ctx context.Context, nameOrID string, timeout time.Duration) error
	CreateVolume(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string) error
	ImageExists(ctx context.Context, image string) bool
	PullImage(ctx context.Context, image string) error
}

// Verify Docker satisfies ContainerRuntime at compile time.
var _ ContainerRuntime = (*Docker)(nil)
