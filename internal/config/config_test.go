package config

import "testing"

func TestIsBuildService(t *testing.T) {
	tests := []struct {
		name    string
		service Service
		want    bool
	}{
		{
			name:    "build service with path set",
			service: Service{Path: "."},
			want:    true,
		},
		{
			name:    "image service is not build service",
			service: Service{Image: "nginx"},
			want:    false,
		},
		{
			name:    "empty service is not build service",
			service: Service{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.service.IsBuildService()
			if got != tt.want {
				t.Errorf("IsBuildService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasHealthcheck(t *testing.T) {
	tests := []struct {
		name    string
		service Service
		want    bool
	}{
		{
			name: "has healthcheck when path is set",
			service: Service{
				Healthcheck: HealthcheckConfig{Path: "/health"},
			},
			want: true,
		},
		{
			name: "has healthcheck when port is set (TCP check)",
			service: Service{
				Port: 8080,
			},
			want: true,
		},
		{
			name: "has healthcheck when both path and port are set",
			service: Service{
				Port:        3000,
				Healthcheck: HealthcheckConfig{Path: "/health"},
			},
			want: true,
		},
		{
			name:    "no healthcheck when both path and port are empty",
			service: Service{},
			want:    false,
		},
		{
			name: "no healthcheck with only image and no port",
			service: Service{
				Image: "nginx",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.service.HasHealthcheck()
			if got != tt.want {
				t.Errorf("HasHealthcheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAllPorts(t *testing.T) {
	tests := []struct {
		name    string
		service Service
		want    map[int]bool // use map for unordered comparison
	}{
		{
			name: "port and expose with deduplication",
			service: Service{
				Port:   3000,
				Expose: []int{3000, 9229},
			},
			want: map[int]bool{3000: true, 9229: true},
		},
		{
			name: "only expose no port",
			service: Service{
				Port:   0,
				Expose: []int{8080},
			},
			want: map[int]bool{8080: true},
		},
		{
			name: "only port no expose",
			service: Service{
				Port: 3000,
			},
			want: map[int]bool{3000: true},
		},
		{
			name:    "no ports at all",
			service: Service{},
			want:    map[int]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.service.GetAllPorts()
			if len(got) != len(tt.want) {
				t.Errorf("GetAllPorts() returned %d ports, want %d", len(got), len(tt.want))
				return
			}
			for _, p := range got {
				if !tt.want[p] {
					t.Errorf("GetAllPorts() returned unexpected port %d", p)
				}
			}
		})
	}
}

func TestDeepCopy(t *testing.T) {
	original := &Config{
		Version: "1",
		Project: ProjectConfig{Name: "test-project"},
		Services: map[string]Service{
			"api": {
				Path:      ".",
				Port:      3000,
				Command:   []string{"npm", "start"},
				DependsOn: []string{"db"},
				Volumes:   []string{"data:/app/data"},
				Secrets:   []string{"api_key"},
				Expose:    []int{3000, 9229},
				Env: map[string]string{
					"NODE_ENV": "production",
					"PORT":     "3000",
				},
				Build: BuildConfig{
					Args: map[string]string{
						"NODE_ENV": "production",
					},
				},
				Dev: DevConfig{
					Command: []string{"npm", "run", "dev"},
					Watch: WatchConfig{
						Paths: []string{"src/", "package.json"},
					},
				},
				Deploy: &ServiceDeployConfig{
					CPU:    512,
					Memory: 1024,
				},
				Resources: &ResourceConfig{
					Memory: "512m",
					CPUs:   "0.5",
				},
			},
		},
	}

	copied := original.DeepCopy()

	// Verify the copy has the same values initially
	if copied.Version != original.Version {
		t.Errorf("DeepCopy Version = %q, want %q", copied.Version, original.Version)
	}
	if copied.Project.Name != original.Project.Name {
		t.Errorf("DeepCopy Project.Name = %q, want %q", copied.Project.Name, original.Project.Name)
	}

	copiedSvc := copied.Services["api"]

	// Now mutate EVERY slice and map in the original
	origSvc := original.Services["api"]
	origSvc.Env["MUTATED"] = "yes"
	origSvc.Build.Args["MUTATED"] = "yes"
	origSvc.Command[0] = "mutated"
	origSvc.DependsOn[0] = "mutated"
	origSvc.Volumes[0] = "mutated"
	origSvc.Secrets[0] = "mutated"
	origSvc.Expose[0] = 9999
	origSvc.Dev.Command[0] = "mutated"
	origSvc.Dev.Watch.Paths[0] = "mutated"
	origSvc.Deploy.CPU = 9999
	origSvc.Resources.Memory = "9999g"
	original.Services["api"] = origSvc

	// Verify the copy is unchanged
	if _, exists := copiedSvc.Env["MUTATED"]; exists {
		t.Error("DeepCopy: Env map was not deep copied")
	}
	if copiedSvc.Env["NODE_ENV"] != "production" {
		t.Error("DeepCopy: Env map value was mutated")
	}

	if _, exists := copiedSvc.Build.Args["MUTATED"]; exists {
		t.Error("DeepCopy: Build.Args map was not deep copied")
	}
	if copiedSvc.Build.Args["NODE_ENV"] != "production" {
		t.Error("DeepCopy: Build.Args map value was mutated")
	}

	if copiedSvc.Command[0] != "npm" {
		t.Errorf("DeepCopy: Command slice was mutated, got %q", copiedSvc.Command[0])
	}

	if copiedSvc.DependsOn[0] != "db" {
		t.Errorf("DeepCopy: DependsOn slice was mutated, got %q", copiedSvc.DependsOn[0])
	}

	if copiedSvc.Volumes[0] != "data:/app/data" {
		t.Errorf("DeepCopy: Volumes slice was mutated, got %q", copiedSvc.Volumes[0])
	}

	if copiedSvc.Secrets[0] != "api_key" {
		t.Errorf("DeepCopy: Secrets slice was mutated, got %q", copiedSvc.Secrets[0])
	}

	if copiedSvc.Expose[0] != 3000 {
		t.Errorf("DeepCopy: Expose slice was mutated, got %d", copiedSvc.Expose[0])
	}

	if copiedSvc.Dev.Command[0] != "npm" {
		t.Errorf("DeepCopy: Dev.Command slice was mutated, got %q", copiedSvc.Dev.Command[0])
	}

	if copiedSvc.Dev.Watch.Paths[0] != "src/" {
		t.Errorf("DeepCopy: Dev.Watch.Paths slice was mutated, got %q", copiedSvc.Dev.Watch.Paths[0])
	}

	if copiedSvc.Deploy.CPU != 512 {
		t.Errorf("DeepCopy: Deploy pointer was not deep copied, CPU = %d", copiedSvc.Deploy.CPU)
	}

	if copiedSvc.Resources.Memory != "512m" {
		t.Errorf("DeepCopy: Resources pointer was not deep copied, Memory = %q", copiedSvc.Resources.Memory)
	}
}

func TestDeepCopy_NilConfig(t *testing.T) {
	var cfg *Config
	copied := cfg.DeepCopy()
	if copied != nil {
		t.Error("DeepCopy of nil config should return nil")
	}
}

func TestWithEnvironment(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Services: map[string]Service{
			"api": {
				Path: ".",
				Port: 3000,
				Env: map[string]string{
					"NODE_ENV":  "development",
					"LOG_LEVEL": "info",
				},
			},
		},
		Environments: map[string]EnvironmentConfig{
			"staging": {
				Services: map[string]ServiceOverrides{
					"api": {
						Env: map[string]string{
							"LOG_LEVEL": "debug",
						},
					},
				},
			},
		},
	}

	t.Run("applies environment overrides", func(t *testing.T) {
		merged, err := cfg.WithEnvironment("staging")
		if err != nil {
			t.Fatalf("WithEnvironment() error = %v", err)
		}

		apiSvc := merged.Services["api"]
		if apiSvc.Env["LOG_LEVEL"] != "debug" {
			t.Errorf("WithEnvironment() LOG_LEVEL = %q, want %q", apiSvc.Env["LOG_LEVEL"], "debug")
		}
		// Non-overridden values should remain
		if apiSvc.Env["NODE_ENV"] != "development" {
			t.Errorf("WithEnvironment() NODE_ENV = %q, want %q", apiSvc.Env["NODE_ENV"], "development")
		}
	})

	t.Run("missing environment returns error", func(t *testing.T) {
		_, err := cfg.WithEnvironment("nonexistent")
		if err == nil {
			t.Error("WithEnvironment() expected error for missing environment, got nil")
		}
	})

	t.Run("original config unchanged after merge", func(t *testing.T) {
		_, err := cfg.WithEnvironment("staging")
		if err != nil {
			t.Fatalf("WithEnvironment() error = %v", err)
		}

		// Original should still have "info"
		if cfg.Services["api"].Env["LOG_LEVEL"] != "info" {
			t.Errorf("Original config was mutated: LOG_LEVEL = %q, want %q",
				cfg.Services["api"].Env["LOG_LEVEL"], "info")
		}
	})
}

func TestHasEnvironment(t *testing.T) {
	cfg := &Config{
		Environments: map[string]EnvironmentConfig{
			"staging":    {},
			"production": {},
		},
	}

	tests := []struct {
		name string
		env  string
		want bool
	}{
		{
			name: "existing environment returns true",
			env:  "staging",
			want: true,
		},
		{
			name: "another existing environment returns true",
			env:  "production",
			want: true,
		},
		{
			name: "non-existing environment returns false",
			env:  "development",
			want: false,
		},
		{
			name: "empty string returns false",
			env:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.HasEnvironment(tt.env)
			if got != tt.want {
				t.Errorf("HasEnvironment(%q) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
