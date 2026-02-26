package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewSnapshotManager(t *testing.T) {
	mgr := NewSnapshotManager()
	if mgr == nil {
		t.Fatal("NewSnapshotManager() returned nil")
	}
	if mgr.baseDir == "" {
		t.Error("baseDir should not be empty")
	}

	// Should be in ~/.cbox/snapshots
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".cbox", "snapshots")
	if mgr.baseDir != expected {
		t.Errorf("baseDir = %v, want %v", mgr.baseDir, expected)
	}
}

func TestSnapshotManager_snapshotDir(t *testing.T) {
	mgr := NewSnapshotManager()

	dir, err := mgr.snapshotDir("myproject", "postgres", "fresh")
	if err != nil {
		t.Fatalf("snapshotDir() error = %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".cbox", "snapshots", "myproject", "postgres", "fresh")
	if dir != expected {
		t.Errorf("snapshotDir() = %v, want %v", dir, expected)
	}
}

func TestSnapshotManager_snapshotDir_Traversal(t *testing.T) {
	mgr := NewSnapshotManager()

	cases := []string{"../../etc/shadow", "../escape", "/absolute", "has spaces", ""}
	for _, name := range cases {
		_, err := mgr.snapshotDir("myproject", "postgres", name)
		if err == nil {
			t.Errorf("snapshotDir(%q) should have returned error", name)
		}
	}
}

func TestSnapshotManager_List_Empty(t *testing.T) {
	mgr := &SnapshotManager{
		baseDir: t.TempDir(),
	}

	// List on non-existent project should return empty, not error
	snapshots, err := mgr.List("nonexistent", "service")
	if err != nil {
		t.Errorf("List() error = %v, want nil", err)
	}
	if len(snapshots) != 0 {
		t.Errorf("List() returned %d snapshots, want 0", len(snapshots))
	}
}

func TestSnapshotManager_Delete_NotFound(t *testing.T) {
	mgr := &SnapshotManager{
		baseDir: t.TempDir(),
	}

	err := mgr.Delete("project", "service", "nonexistent")
	if err == nil {
		t.Error("Delete() should return error for non-existent snapshot")
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := FormatSize(tt.bytes); got != tt.expected {
				t.Errorf("FormatSize(%d) = %v, want %v", tt.bytes, got, tt.expected)
			}
		})
	}
}

func TestSnapshotManager_ListWithMeta(t *testing.T) {
	tempDir := t.TempDir()
	mgr := &SnapshotManager{
		baseDir: tempDir,
	}

	// Create a fake snapshot directory with metadata
	snapshotDir := filepath.Join(tempDir, "testproject", "db", "snapshot1")
	err := os.MkdirAll(snapshotDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create snapshot dir: %v", err)
	}

	metaContent := `{
		"name": "snapshot1",
		"db_type": "postgres",
		"service": "db",
		"project": "testproject",
		"created": "2024-01-15T10:30:00Z",
		"size": 1024
	}`
	err = os.WriteFile(filepath.Join(snapshotDir, "meta.json"), []byte(metaContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write meta.json: %v", err)
	}

	// List snapshots
	snapshots, err := mgr.List("testproject", "db")
	if err != nil {
		t.Errorf("List() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Errorf("List() returned %d snapshots, want 1", len(snapshots))
	}

	if len(snapshots) > 0 {
		s := snapshots[0]
		if s.Name != "snapshot1" {
			t.Errorf("Name = %v, want snapshot1", s.Name)
		}
		if s.DBType != "postgres" {
			t.Errorf("DBType = %v, want postgres", s.DBType)
		}
		if s.Size != 1024 {
			t.Errorf("Size = %v, want 1024", s.Size)
		}
	}
}

func TestSnapshotManager_DeleteExisting(t *testing.T) {
	tempDir := t.TempDir()
	mgr := &SnapshotManager{
		baseDir: tempDir,
	}

	// Create a fake snapshot directory
	snapshotDir := filepath.Join(tempDir, "testproject", "db", "todelete")
	err := os.MkdirAll(snapshotDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create snapshot dir: %v", err)
	}

	// Create dummy files
	err = os.WriteFile(filepath.Join(snapshotDir, "meta.json"), []byte("{}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write meta.json: %v", err)
	}

	// Delete should succeed
	err = mgr.Delete("testproject", "db", "todelete")
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	// Directory should be gone
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Error("Snapshot directory should be deleted")
	}
}
