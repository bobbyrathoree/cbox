package db

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// SnapshotMeta holds metadata about a snapshot
type SnapshotMeta struct {
	Name      string    `json:"name"`
	DBType    string    `json:"db_type"`
	DBName    string    `json:"db_name,omitempty"` // Database name (e.g., POSTGRES_DB)
	Service   string    `json:"service"`
	Project   string    `json:"project"`
	Created   time.Time `json:"created"`
	Size      int64     `json:"size"`
	Container string    `json:"container"`
}

// SnapshotManager handles database snapshots
type SnapshotManager struct {
	baseDir string
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager() *SnapshotManager {
	home, _ := os.UserHomeDir()
	return &SnapshotManager{
		baseDir: filepath.Join(home, ".cbox", "snapshots"),
	}
}

var validSnapshotName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// snapshotDir returns the directory for a specific snapshot
func (m *SnapshotManager) snapshotDir(project, service, name string) (string, error) {
	for _, component := range []string{project, service, name} {
		if component != filepath.Base(component) || !validSnapshotName.MatchString(component) {
			return "", fmt.Errorf("invalid name %q: must be alphanumeric with ._- only", component)
		}
	}
	return filepath.Join(m.baseDir, project, service, name), nil
}

// Create creates a new snapshot
func (m *SnapshotManager) Create(ctx context.Context, project, service, container, name string, dbType DBType, dbName string) error {
	if dbType == Unknown || dbType == Redis {
		return fmt.Errorf("snapshot not supported for %s", dbType)
	}

	dir, err := m.snapshotDir(project, service, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	// Get dump command
	dumpCmd := DumpCommand(dbType, dbName)
	if dumpCmd == nil {
		return fmt.Errorf("dump not supported for %s", dbType)
	}

	// Create snapshot file
	ext := SnapshotExtension(dbType)
	snapshotFile := filepath.Join(dir, "data"+ext)

	file, err := os.Create(snapshotFile)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()

	// For non-gzipped formats, add gzip compression
	var writer io.WriteCloser
	if dbType == Postgres || dbType == MongoDB {
		// Already compressed by the dump command
		writer = file
	} else {
		// Add gzip compression for MySQL
		gzWriter := gzip.NewWriter(file)
		defer gzWriter.Close()
		writer = gzWriter
	}

	// Run docker exec with dump command
	args := append([]string{"exec", container}, dumpCmd...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = writer
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Close writer to flush
	if w, ok := writer.(*gzip.Writer); ok {
		w.Close()
	}

	// Get file size
	stat, _ := os.Stat(snapshotFile)
	size := int64(0)
	if stat != nil {
		size = stat.Size()
	}

	// Write metadata
	meta := SnapshotMeta{
		Name:      name,
		DBType:    dbType.String(),
		DBName:    dbName,
		Service:   service,
		Project:   project,
		Created:   time.Now(),
		Size:      size,
		Container: container,
	}

	metaFile := filepath.Join(dir, "meta.json")
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaFile, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// Restore restores a snapshot
func (m *SnapshotManager) Restore(ctx context.Context, project, service, container, name string, dbType DBType) error {
	if dbType == Unknown || dbType == Redis {
		return fmt.Errorf("restore not supported for %s", dbType)
	}

	dir, err := m.snapshotDir(project, service, name)
	if err != nil {
		return err
	}

	// Check if snapshot exists and read metadata
	metaFile := filepath.Join(dir, "meta.json")
	metaData, err := os.ReadFile(metaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot '%s' not found", name)
		}
		return fmt.Errorf("failed to read snapshot metadata: %w", err)
	}

	var meta SnapshotMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return fmt.Errorf("failed to parse snapshot metadata: %w", err)
	}

	// Get restore command with the stored database name
	restoreCmd := RestoreCommand(dbType, meta.DBName)
	if restoreCmd == nil {
		return fmt.Errorf("restore not supported for %s", dbType)
	}

	// Find snapshot file
	ext := SnapshotExtension(dbType)
	snapshotFile := filepath.Join(dir, "data"+ext)

	file, err := os.Open(snapshotFile)
	if err != nil {
		return fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer file.Close()

	// Decompress if needed
	var reader io.Reader
	if dbType == Postgres || dbType == MongoDB {
		// Already decompressed by the restore command for pg_restore
		// For MongoDB, mongorestore handles gzip
		reader = file
	} else {
		// MySQL needs decompression
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("failed to decompress snapshot: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// Run docker exec with restore command
	args := append([]string{"exec", "-i", container}, restoreCmd...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = reader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	return nil
}

// List returns all snapshots for a project/service
func (m *SnapshotManager) List(project, service string) ([]SnapshotMeta, error) {
	dir := filepath.Join(m.baseDir, project, service)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var snapshots []SnapshotMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metaFile := filepath.Join(dir, entry.Name(), "meta.json")
		data, err := os.ReadFile(metaFile)
		if err != nil {
			continue
		}

		var meta SnapshotMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		snapshots = append(snapshots, meta)
	}

	// Sort by creation time (newest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Created.After(snapshots[j].Created)
	})

	return snapshots, nil
}

// Delete deletes a snapshot
func (m *SnapshotManager) Delete(project, service, name string) error {
	dir, err := m.snapshotDir(project, service, name)
	if err != nil {
		return err
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("snapshot '%s' not found", name)
	}

	return os.RemoveAll(dir)
}

// FormatSize formats bytes into human-readable string
func FormatSize(bytes int64) string {
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
