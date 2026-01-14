// Package db provides database utilities for snapshots and shells.
package db

import (
	"strings"
)

// DBType represents a database type
type DBType int

const (
	Unknown DBType = iota
	Postgres
	MySQL
	MongoDB
	Redis
)

func (t DBType) String() string {
	switch t {
	case Postgres:
		return "postgres"
	case MySQL:
		return "mysql"
	case MongoDB:
		return "mongodb"
	case Redis:
		return "redis"
	default:
		return "unknown"
	}
}

// DetectDBType determines the database type from an image name
func DetectDBType(image string) DBType {
	lower := strings.ToLower(image)

	switch {
	case strings.Contains(lower, "postgres"):
		return Postgres
	case strings.Contains(lower, "mysql") || strings.Contains(lower, "mariadb"):
		return MySQL
	case strings.Contains(lower, "mongo"):
		return MongoDB
	case strings.Contains(lower, "redis"):
		return Redis
	default:
		return Unknown
	}
}

// ShellCommand returns the appropriate shell command for a database type
func ShellCommand(dbType DBType) []string {
	switch dbType {
	case Postgres:
		return []string{"psql", "-U", "postgres"}
	case MySQL:
		return []string{"mysql", "-u", "root"}
	case MongoDB:
		return []string{"mongosh"}
	case Redis:
		return []string{"redis-cli"}
	default:
		return []string{"sh"}
	}
}

// DumpCommand returns the command to dump a database
func DumpCommand(dbType DBType) []string {
	switch dbType {
	case Postgres:
		return []string{"pg_dump", "-U", "postgres", "-Fc"}
	case MySQL:
		return []string{"mysqldump", "-u", "root", "--all-databases"}
	case MongoDB:
		return []string{"mongodump", "--archive", "--gzip"}
	case Redis:
		return []string{"redis-cli", "BGSAVE"}
	default:
		return nil
	}
}

// RestoreCommand returns the command to restore a database
func RestoreCommand(dbType DBType) []string {
	switch dbType {
	case Postgres:
		return []string{"pg_restore", "-U", "postgres", "-d", "postgres", "-c"}
	case MySQL:
		return []string{"mysql", "-u", "root"}
	case MongoDB:
		return []string{"mongorestore", "--archive", "--gzip", "--drop"}
	default:
		return nil
	}
}

// SnapshotExtension returns the file extension for snapshots
func SnapshotExtension(dbType DBType) string {
	switch dbType {
	case Postgres:
		return ".dump"
	case MySQL:
		return ".sql.gz"
	case MongoDB:
		return ".archive.gz"
	case Redis:
		return ".rdb"
	default:
		return ".backup"
	}
}
