package runtime

import (
	"testing"
)

func TestCheckPortAvailable(t *testing.T) {
	// Port 0 lets the OS assign a free port
	// We use a high port that's likely available
	testPort := 59999

	// First check should usually succeed (port likely available)
	err := CheckPortAvailable(testPort)
	// Don't fail if port is in use - it's system dependent
	if err != nil {
		t.Logf("Port %d is in use (expected in some environments): %v", testPort, err)
	}
}

func TestFindAvailablePort(t *testing.T) {
	// Try to find an available port starting from a high number
	startPort := 58000
	maxAttempts := 100

	port, err := FindAvailablePort(startPort, maxAttempts)
	if err != nil {
		t.Logf("Could not find available port in range %d-%d: %v", startPort, startPort+maxAttempts, err)
		return
	}

	if port < startPort || port >= startPort+maxAttempts {
		t.Errorf("FindAvailablePort() = %d, want in range [%d, %d)", port, startPort, startPort+maxAttempts)
	}

	// Verify the found port is actually available
	err = CheckPortAvailable(port)
	if err != nil {
		t.Errorf("FindAvailablePort() returned port %d but it's not available: %v", port, err)
	}
}

func TestFindAvailablePort_SmallRange(t *testing.T) {
	// Test with a small range - if first port is available, should return it
	startPort := 57000
	maxAttempts := 1

	port, err := FindAvailablePort(startPort, maxAttempts)
	if err != nil {
		// Port might be in use, that's okay
		t.Logf("Port %d not available: %v", startPort, err)
		return
	}

	if port != startPort {
		t.Errorf("With maxAttempts=1, should return startPort if available, got %d", port)
	}
}

func TestPortMapping(t *testing.T) {
	pm := PortMapping{
		HostPort:      3000,
		ContainerPort: 8080,
		Protocol:      "tcp",
	}

	if pm.HostPort != 3000 {
		t.Errorf("HostPort = %d, want 3000", pm.HostPort)
	}
	if pm.ContainerPort != 8080 {
		t.Errorf("ContainerPort = %d, want 8080", pm.ContainerPort)
	}
	if pm.Protocol != "tcp" {
		t.Errorf("Protocol = %s, want tcp", pm.Protocol)
	}
}

func TestVolumeMount(t *testing.T) {
	vm := VolumeMount{
		Name:      "data",
		MountPath: "/var/lib/data",
		ReadOnly:  false,
	}

	if vm.Name != "data" {
		t.Errorf("Name = %s, want data", vm.Name)
	}
	if vm.MountPath != "/var/lib/data" {
		t.Errorf("MountPath = %s, want /var/lib/data", vm.MountPath)
	}
	if vm.ReadOnly != false {
		t.Errorf("ReadOnly = %v, want false", vm.ReadOnly)
	}
}

func TestBindMount(t *testing.T) {
	bm := BindMount{
		HostPath:      "/home/user/app",
		ContainerPath: "/app",
		ReadOnly:      true,
	}

	if bm.HostPath != "/home/user/app" {
		t.Errorf("HostPath = %s, want /home/user/app", bm.HostPath)
	}
	if bm.ContainerPath != "/app" {
		t.Errorf("ContainerPath = %s, want /app", bm.ContainerPath)
	}
	if bm.ReadOnly != true {
		t.Errorf("ReadOnly = %v, want true", bm.ReadOnly)
	}
}

func TestContainerConfig(t *testing.T) {
	cfg := ContainerConfig{
		Name:    "myapp",
		Image:   "nginx:latest",
		Network: "cbox_myproject",
		Ports: []PortMapping{
			{HostPort: 80, ContainerPort: 80},
		},
		Env: map[string]string{
			"NODE_ENV": "production",
		},
		Labels: map[string]string{
			"cbox.project": "myproject",
		},
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %s, want myapp", cfg.Name)
	}
	if cfg.Image != "nginx:latest" {
		t.Errorf("Image = %s, want nginx:latest", cfg.Image)
	}
	if len(cfg.Ports) != 1 {
		t.Errorf("len(Ports) = %d, want 1", len(cfg.Ports))
	}
	if cfg.Env["NODE_ENV"] != "production" {
		t.Errorf("Env[NODE_ENV] = %s, want production", cfg.Env["NODE_ENV"])
	}
}

func TestContainer(t *testing.T) {
	c := Container{
		ID:     "abc123",
		Name:   "myapp_web",
		Image:  "nginx:latest",
		Status: "running",
		Ports:  []string{"80/tcp"},
		Health: "healthy",
	}

	if c.ID != "abc123" {
		t.Errorf("ID = %s, want abc123", c.ID)
	}
	if c.Name != "myapp_web" {
		t.Errorf("Name = %s, want myapp_web", c.Name)
	}
	if c.Status != "running" {
		t.Errorf("Status = %s, want running", c.Status)
	}
}

func TestHealthcheckConfig(t *testing.T) {
	hc := HealthcheckConfig{
		Test:        []string{"CMD", "curl", "-f", "http://localhost/health"},
		Interval:    30,
		Timeout:     10,
		Retries:     3,
		StartPeriod: 5,
	}

	if len(hc.Test) != 4 {
		t.Errorf("len(Test) = %d, want 4", len(hc.Test))
	}
	if hc.Test[0] != "CMD" {
		t.Errorf("Test[0] = %s, want CMD", hc.Test[0])
	}
	if hc.Retries != 3 {
		t.Errorf("Retries = %d, want 3", hc.Retries)
	}
}
