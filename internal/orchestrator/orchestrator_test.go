package orchestrator

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrathore/cbox/internal/config"
	"github.com/bobbyrathore/cbox/internal/output"
	"github.com/bobbyrathore/cbox/internal/runtime"
)

// mockRuntime records all calls for verification.
type mockRuntime struct {
	networksCreated   []string
	containersCreated []string
	containersStarted []string
	containersStopped []string
	containersRemoved []string
	networksRemoved   []string
	volumesCreated    []string
	volumesRemoved    []string
	imagesPulled      []string

	existingContainers map[string]bool
	runningContainers  map[string]bool
	existingImages     map[string]bool
	containers         []runtime.Container
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		existingContainers: make(map[string]bool),
		runningContainers:  make(map[string]bool),
		existingImages:     make(map[string]bool),
	}
}

func (m *mockRuntime) CreateNetwork(_ context.Context, name string) error {
	m.networksCreated = append(m.networksCreated, name)
	return nil
}

func (m *mockRuntime) RemoveNetwork(_ context.Context, name string) error {
	m.networksRemoved = append(m.networksRemoved, name)
	return nil
}

func (m *mockRuntime) CreateContainer(_ context.Context, cfg runtime.ContainerConfig) (string, error) {
	m.containersCreated = append(m.containersCreated, cfg.Name)
	m.existingContainers[cfg.Name] = true
	return "mock-id-" + cfg.Name, nil
}

func (m *mockRuntime) StartContainer(_ context.Context, nameOrID string) error {
	m.containersStarted = append(m.containersStarted, nameOrID)
	m.runningContainers[nameOrID] = true
	return nil
}

func (m *mockRuntime) StopContainer(_ context.Context, nameOrID string, _ time.Duration) error {
	m.containersStopped = append(m.containersStopped, nameOrID)
	delete(m.runningContainers, nameOrID)
	return nil
}

func (m *mockRuntime) RestartContainer(_ context.Context, nameOrID string, _ time.Duration) error {
	m.containersStopped = append(m.containersStopped, nameOrID)
	m.containersStarted = append(m.containersStarted, nameOrID)
	return nil
}

func (m *mockRuntime) RemoveContainer(_ context.Context, nameOrID string) error {
	m.containersRemoved = append(m.containersRemoved, nameOrID)
	delete(m.existingContainers, nameOrID)
	delete(m.runningContainers, nameOrID)
	return nil
}

func (m *mockRuntime) ContainerExists(_ context.Context, nameOrID string) bool {
	return m.existingContainers[nameOrID]
}

func (m *mockRuntime) IsContainerRunning(_ context.Context, nameOrID string) bool {
	return m.runningContainers[nameOrID]
}

func (m *mockRuntime) ListContainers(_ context.Context, _ map[string]string, _ bool) ([]runtime.Container, error) {
	return m.containers, nil
}

func (m *mockRuntime) GetContainerStats(_ context.Context, _ map[string]string) (map[string]runtime.ContainerStats, error) {
	return map[string]runtime.ContainerStats{}, nil
}

func (m *mockRuntime) ContainerLogs(_ context.Context, _ string, _ bool, _ int) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockRuntime) ContainerExec(_ context.Context, _ string, _ []string, _, _ bool) error {
	return nil
}

func (m *mockRuntime) ContainerExecWithOutput(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}

func (m *mockRuntime) WaitHealthy(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (m *mockRuntime) CreateVolume(_ context.Context, name string) error {
	m.volumesCreated = append(m.volumesCreated, name)
	return nil
}

func (m *mockRuntime) RemoveVolume(_ context.Context, name string) error {
	m.volumesRemoved = append(m.volumesRemoved, name)
	return nil
}

func (m *mockRuntime) ImageExists(_ context.Context, image string) bool {
	return m.existingImages[image]
}

func (m *mockRuntime) PullImage(_ context.Context, image string) error {
	m.imagesPulled = append(m.imagesPulled, image)
	m.existingImages[image] = true
	return nil
}

// ---------- Tests for naming helpers ----------

func TestContainerName(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		namespace string
		service   string
		want      string
	}{
		{
			name:    "no namespace",
			project: "project",
			service: "svc",
			want:    "project_svc",
		},
		{
			name:      "with namespace",
			project:   "project",
			namespace: "ns",
			service:   "svc",
			want:      "ns-project_svc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Project:  config.ProjectConfig{Name: tt.project},
				Services: map[string]config.Service{},
			}
			console := output.NewWithOptions(false, true)
			orch := &Orchestrator{
				config:    cfg,
				console:   console,
				runtime:   newMockRuntime(),
				namespace: tt.namespace,
			}

			got := orch.containerName(tt.service)
			if got != tt.want {
				t.Errorf("containerName(%q) = %q, want %q", tt.service, got, tt.want)
			}
		})
	}
}

func TestNetworkName(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		namespace string
		want      string
	}{
		{
			name:    "no namespace",
			project: "project",
			want:    "cbox_project",
		},
		{
			name:      "with namespace",
			project:   "project",
			namespace: "ns",
			want:      "cbox_ns-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Project:  config.ProjectConfig{Name: tt.project},
				Services: map[string]config.Service{},
			}
			console := output.NewWithOptions(false, true)
			orch := &Orchestrator{
				config:    cfg,
				console:   console,
				runtime:   newMockRuntime(),
				namespace: tt.namespace,
			}

			got := orch.networkName()
			if got != tt.want {
				t.Errorf("networkName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------- Tests for resolveStartOrder ----------

func TestResolveStartOrder_NoDeps(t *testing.T) {
	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "test"},
		Services: map[string]config.Service{
			"api":    {},
			"worker": {},
			"db":     {},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{config: cfg, console: console, runtime: newMockRuntime()}

	order, err := orch.resolveStartOrder([]string{"api", "worker", "db"}, true)
	if err != nil {
		t.Fatalf("resolveStartOrder returned error: %v", err)
	}

	// All services with no dependencies should be in a single level (level 0)
	if len(order) != 1 {
		t.Fatalf("expected 1 level, got %d: %v", len(order), order)
	}

	// All three services should be present in level 0
	got := make([]string, len(order[0]))
	copy(got, order[0])
	sort.Strings(got)
	want := []string{"api", "db", "worker"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("level 0 = %v, want %v", got, want)
	}
}

func TestResolveStartOrder_LinearDeps(t *testing.T) {
	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "test"},
		Services: map[string]config.Service{
			"A": {DependsOn: []string{"B"}},
			"B": {DependsOn: []string{"C"}},
			"C": {},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{config: cfg, console: console, runtime: newMockRuntime()}

	order, err := orch.resolveStartOrder([]string{"A"}, true)
	if err != nil {
		t.Fatalf("resolveStartOrder returned error: %v", err)
	}

	// Expect 3 levels: [[C], [B], [A]]
	if len(order) != 3 {
		t.Fatalf("expected 3 levels, got %d: %v", len(order), order)
	}

	if len(order[0]) != 1 || order[0][0] != "C" {
		t.Errorf("level 0 = %v, want [C]", order[0])
	}
	if len(order[1]) != 1 || order[1][0] != "B" {
		t.Errorf("level 1 = %v, want [B]", order[1])
	}
	if len(order[2]) != 1 || order[2][0] != "A" {
		t.Errorf("level 2 = %v, want [A]", order[2])
	}
}

func TestResolveStartOrder_CircularDeps(t *testing.T) {
	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "test"},
		Services: map[string]config.Service{
			"A": {DependsOn: []string{"B"}},
			"B": {DependsOn: []string{"A"}},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{config: cfg, console: console, runtime: newMockRuntime()}

	_, err := orch.resolveStartOrder([]string{"A"}, true)
	if err == nil {
		t.Fatal("expected circular dependency error, got nil")
	}
	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("error = %q, want it to contain 'circular dependency'", err.Error())
	}
}

// ---------- Tests for Up / Down flows ----------

func TestUp_CreatesNetworkAndContainers(t *testing.T) {
	mock := newMockRuntime()
	// Mark images as existing so Up doesn't try to pull (they're image-based services)
	mock.existingImages["postgres:15"] = true
	mock.existingImages["redis:7"] = true

	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "myapp"},
		Services: map[string]config.Service{
			"db":    {Image: "postgres:15"},
			"cache": {Image: "redis:7"},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{
		config:  cfg,
		console: console,
		runtime: mock,
	}

	err := orch.Up(context.Background(), UpOptions{})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	// Verify network was created
	if len(mock.networksCreated) != 1 || mock.networksCreated[0] != "cbox_myapp" {
		t.Errorf("networksCreated = %v, want [cbox_myapp]", mock.networksCreated)
	}

	// Verify both containers were created and started
	if len(mock.containersCreated) != 2 {
		t.Errorf("expected 2 containers created, got %d: %v", len(mock.containersCreated), mock.containersCreated)
	}
	if len(mock.containersStarted) != 2 {
		t.Errorf("expected 2 containers started, got %d: %v", len(mock.containersStarted), mock.containersStarted)
	}

	// Verify container names
	createdSet := make(map[string]bool)
	for _, name := range mock.containersCreated {
		createdSet[name] = true
	}
	if !createdSet["myapp_db"] {
		t.Error("expected myapp_db to be created")
	}
	if !createdSet["myapp_cache"] {
		t.Error("expected myapp_cache to be created")
	}
}

func TestDown_StopsAndRemovesContainers(t *testing.T) {
	mock := newMockRuntime()
	// Simulate existing running containers that ListContainers would return
	mock.containers = []runtime.Container{
		{ID: "abc123", Name: "myapp_db", Image: "postgres:15", Status: "Up 5 minutes"},
		{ID: "def456", Name: "myapp_cache", Image: "redis:7", Status: "Up 5 minutes"},
	}

	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "myapp"},
		Services: map[string]config.Service{
			"db":    {Image: "postgres:15"},
			"cache": {Image: "redis:7"},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{
		config:  cfg,
		console: console,
		runtime: mock,
	}

	err := orch.Down(context.Background(), DownOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Down returned error: %v", err)
	}

	// Verify both containers were stopped
	if len(mock.containersStopped) != 2 {
		t.Errorf("expected 2 containers stopped, got %d: %v", len(mock.containersStopped), mock.containersStopped)
	}

	// Verify both containers were removed
	if len(mock.containersRemoved) != 2 {
		t.Errorf("expected 2 containers removed, got %d: %v", len(mock.containersRemoved), mock.containersRemoved)
	}

	// Verify network was removed
	if len(mock.networksRemoved) != 1 || mock.networksRemoved[0] != "cbox_myapp" {
		t.Errorf("networksRemoved = %v, want [cbox_myapp]", mock.networksRemoved)
	}
}

func TestDown_WithVolumes(t *testing.T) {
	mock := newMockRuntime()
	mock.containers = []runtime.Container{}

	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "myapp"},
		Services: map[string]config.Service{
			"db": {Image: "postgres:15"},
		},
		Volumes: map[string]config.Volume{
			"pgdata": {},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{
		config:  cfg,
		console: console,
		runtime: mock,
	}

	err := orch.Down(context.Background(), DownOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Down returned error: %v", err)
	}

	// Verify volume was removed
	if len(mock.volumesRemoved) != 1 || mock.volumesRemoved[0] != "myapp_pgdata" {
		t.Errorf("volumesRemoved = %v, want [myapp_pgdata]", mock.volumesRemoved)
	}
}

func TestUp_WithNamespace(t *testing.T) {
	mock := newMockRuntime()
	mock.existingImages["postgres:15"] = true

	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "myapp"},
		Services: map[string]config.Service{
			"db": {Image: "postgres:15"},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{
		config:    cfg,
		console:   console,
		runtime:   mock,
		namespace: "staging",
	}

	err := orch.Up(context.Background(), UpOptions{})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	// Verify namespaced network
	if len(mock.networksCreated) != 1 || mock.networksCreated[0] != "cbox_staging-myapp" {
		t.Errorf("networksCreated = %v, want [cbox_staging-myapp]", mock.networksCreated)
	}

	// Verify namespaced container name
	if len(mock.containersCreated) != 1 || mock.containersCreated[0] != "staging-myapp_db" {
		t.Errorf("containersCreated = %v, want [staging-myapp_db]", mock.containersCreated)
	}
}

func TestUp_SkipsAlreadyRunning(t *testing.T) {
	mock := newMockRuntime()
	// Mark container as already existing and running
	mock.existingContainers["myapp_db"] = true
	mock.runningContainers["myapp_db"] = true
	mock.existingImages["postgres:15"] = true

	cfg := &config.Config{
		Project: config.ProjectConfig{Name: "myapp"},
		Services: map[string]config.Service{
			"db": {Image: "postgres:15"},
		},
	}
	console := output.NewWithOptions(false, true)
	orch := &Orchestrator{
		config:  cfg,
		console: console,
		runtime: mock,
	}

	err := orch.Up(context.Background(), UpOptions{})
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}

	// Container already running, so it should NOT be created or started again
	if len(mock.containersCreated) != 0 {
		t.Errorf("expected 0 containers created (already running), got %d: %v", len(mock.containersCreated), mock.containersCreated)
	}
}
