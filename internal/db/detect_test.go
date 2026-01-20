package db

import (
	"testing"
)

func TestDetectDBType(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		expected DBType
	}{
		{"postgres official", "postgres:15", Postgres},
		{"postgres alpine", "postgres:15-alpine", Postgres},
		{"postgresql", "postgresql:latest", Postgres},
		{"mysql official", "mysql:8", MySQL},
		{"mysql alpine", "mysql:8-alpine", MySQL},
		{"mariadb", "mariadb:10", MySQL},
		{"mongo official", "mongo:6", MongoDB},
		{"mongodb", "mongodb:latest", MongoDB},
		{"redis official", "redis:7", Redis},
		{"redis alpine", "redis:7-alpine", Redis},
		{"unknown nginx", "nginx:latest", Unknown},
		{"unknown node", "node:18", Unknown},
		{"case insensitive", "POSTGRES:15", Postgres},
		{"mixed case", "PostgreSQL:latest", Postgres},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectDBType(tt.image)
			if result != tt.expected {
				t.Errorf("DetectDBType(%q) = %v, want %v", tt.image, result, tt.expected)
			}
		})
	}
}

func TestDBType_String(t *testing.T) {
	tests := []struct {
		dbType   DBType
		expected string
	}{
		{Postgres, "postgres"},
		{MySQL, "mysql"},
		{MongoDB, "mongodb"},
		{Redis, "redis"},
		{Unknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.dbType.String(); got != tt.expected {
				t.Errorf("DBType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShellCommand(t *testing.T) {
	tests := []struct {
		dbType      DBType
		expectedCmd string
	}{
		{Postgres, "psql"},
		{MySQL, "mysql"},
		{MongoDB, "mongosh"},
		{Redis, "redis-cli"},
		{Unknown, "sh"},
	}

	for _, tt := range tests {
		t.Run(tt.dbType.String(), func(t *testing.T) {
			cmd := ShellCommand(tt.dbType)
			if len(cmd) == 0 || cmd[0] != tt.expectedCmd {
				t.Errorf("ShellCommand(%v)[0] = %v, want %v", tt.dbType, cmd[0], tt.expectedCmd)
			}
		})
	}
}

func TestDumpCommand(t *testing.T) {
	tests := []struct {
		dbType      DBType
		expectedCmd string
		shouldBeNil bool
	}{
		{Postgres, "pg_dump", false},
		{MySQL, "mysqldump", false},
		{MongoDB, "mongodump", false},
		{Redis, "redis-cli", false},
		{Unknown, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.dbType.String(), func(t *testing.T) {
			cmd := DumpCommand(tt.dbType, "")
			if tt.shouldBeNil {
				if cmd != nil {
					t.Errorf("DumpCommand(%v) = %v, want nil", tt.dbType, cmd)
				}
			} else {
				if len(cmd) == 0 || cmd[0] != tt.expectedCmd {
					t.Errorf("DumpCommand(%v)[0] = %v, want %v", tt.dbType, cmd[0], tt.expectedCmd)
				}
			}
		})
	}
}

func TestRestoreCommand(t *testing.T) {
	tests := []struct {
		dbType      DBType
		expectedCmd string
		shouldBeNil bool
	}{
		{Postgres, "pg_restore", false},
		{MySQL, "mysql", false},
		{MongoDB, "mongorestore", false},
		{Redis, "", true},
		{Unknown, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.dbType.String(), func(t *testing.T) {
			cmd := RestoreCommand(tt.dbType, "")
			if tt.shouldBeNil {
				if cmd != nil {
					t.Errorf("RestoreCommand(%v) = %v, want nil", tt.dbType, cmd)
				}
			} else {
				if len(cmd) == 0 || cmd[0] != tt.expectedCmd {
					t.Errorf("RestoreCommand(%v)[0] = %v, want %v", tt.dbType, cmd[0], tt.expectedCmd)
				}
			}
		})
	}
}

func TestSnapshotExtension(t *testing.T) {
	tests := []struct {
		dbType   DBType
		expected string
	}{
		{Postgres, ".dump"},
		{MySQL, ".sql.gz"},
		{MongoDB, ".archive.gz"},
		{Redis, ".rdb"},
		{Unknown, ".backup"},
	}

	for _, tt := range tests {
		t.Run(tt.dbType.String(), func(t *testing.T) {
			if got := SnapshotExtension(tt.dbType); got != tt.expected {
				t.Errorf("SnapshotExtension(%v) = %v, want %v", tt.dbType, got, tt.expected)
			}
		})
	}
}
