package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewServiceDefinition(t *testing.T) {
	root := "/srv/services"

	tests := []struct {
		name           string
		service        string
		wantOK         bool
		wantContainer  string
		wantConfigPath string
		wantHealthURL  string
	}{
		{
			name:           "hello-service",
			service:        "hello-service",
			wantOK:         true,
			wantContainer:  "hello-service",
			wantConfigPath: filepath.Join(root, "hello-service", "config.yaml"),
			wantHealthURL:  "http://localhost:18080/health",
		},
		{
			name:           "config-service",
			service:        "config-service",
			wantOK:         true,
			wantContainer:  "config-service",
			wantConfigPath: filepath.Join(root, "config-service", "config", "config.yaml"),
		},
		{
			name:           "test-service",
			service:        "test-service",
			wantOK:         true,
			wantContainer:  "test-service",
			wantConfigPath: filepath.Join(root, "test-service", "config", "config.yaml"),
		},
		{
			name:           "data-service",
			service:        "data-service",
			wantOK:         true,
			wantContainer:  "data-service",
			wantConfigPath: filepath.Join(root, "data-service", "config", "config.yaml"),
			wantHealthURL:  "http://data-service:8081/health",
		},
		{
			name:    "unknown service",
			service: "missing-service",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(healthURLEnvKey(tc.service), "")
			def, ok := NewServiceDefinition(root, tc.service)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if def.Container != tc.wantContainer {
				t.Errorf("Container = %q, want %q", def.Container, tc.wantContainer)
			}
			if def.ConfigPath != tc.wantConfigPath {
				t.Errorf("ConfigPath = %q, want %q", def.ConfigPath, tc.wantConfigPath)
			}
			if def.HealthURL != tc.wantHealthURL {
				t.Errorf("HealthURL = %q, want %q", def.HealthURL, tc.wantHealthURL)
			}
		})
	}
}

func TestNewServiceDefinitionHealthURLOverride(t *testing.T) {
	t.Setenv(healthURLEnvKey("data-service"), "http://localhost:8081/health")

	def, ok := NewServiceDefinition("/srv/services", "data-service")
	if !ok {
		t.Fatal("data-service not found")
	}
	if def.HealthURL != "http://localhost:8081/health" {
		t.Errorf("HealthURL = %q, want override %q", def.HealthURL, "http://localhost:8081/health")
	}
}

func TestBackupFile(t *testing.T) {
	t.Run("copies content", func(t *testing.T) {
		dir := t.TempDir()
		source := filepath.Join(dir, "source.txt")
		backup := filepath.Join(dir, "backup.txt")

		if err := os.WriteFile(source, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}

		if err := BackupFile(source, backup); err != nil {
			t.Fatalf("BackupFile() error = %v", err)
		}

		got, err := os.ReadFile(backup)
		if err != nil {
			t.Fatalf("read backup: %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("backup content = %q, want %q", got, "hello")
		}
	})

	t.Run("missing source", func(t *testing.T) {
		dir := t.TempDir()
		err := BackupFile(
			filepath.Join(dir, "missing.txt"),
			filepath.Join(dir, "backup.txt"),
		)
		if err == nil {
			t.Fatal("expected error for missing source, got nil")
		}
	})
}

func TestReplaceConfig(t *testing.T) {
	t.Run("replaces existing target and keeps backup", func(t *testing.T) {
		dir := t.TempDir()
		staged := filepath.Join(dir, "staged.yaml")
		target := filepath.Join(dir, "target.yaml")
		backup := filepath.Join(dir, "target.yaml.bak")

		mustWrite(t, staged, "new")
		mustWrite(t, target, "old")

		if err := ReplaceConfig(staged, target, backup); err != nil {
			t.Fatalf("ReplaceConfig() error = %v", err)
		}

		if got := mustRead(t, target); got != "new" {
			t.Errorf("target = %q, want %q", got, "new")
		}
		if got := mustRead(t, backup); got != "old" {
			t.Errorf("backup = %q, want %q", got, "old")
		}
	})

	t.Run("creates target when missing", func(t *testing.T) {
		dir := t.TempDir()
		staged := filepath.Join(dir, "staged.yaml")
		target := filepath.Join(dir, "target.yaml")
		backup := filepath.Join(dir, "target.yaml.bak")

		mustWrite(t, staged, "new")

		if err := ReplaceConfig(staged, target, backup); err != nil {
			t.Fatalf("ReplaceConfig() error = %v", err)
		}
		if got := mustRead(t, target); got != "new" {
			t.Errorf("target = %q, want %q", got, "new")
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Errorf("backup should not exist when target was missing, stat err = %v", err)
		}
	})

	t.Run("missing staged file", func(t *testing.T) {
		dir := t.TempDir()
		err := ReplaceConfig(
			filepath.Join(dir, "missing.yaml"),
			filepath.Join(dir, "target.yaml"),
			filepath.Join(dir, "target.yaml.bak"),
		)
		if err == nil {
			t.Fatal("expected error for missing staged file, got nil")
		}
	})
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
