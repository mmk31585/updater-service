package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewDeployer(t *testing.T) {
	d := NewDeployer()
	if d == nil {
		t.Fatal("NewDeployer returned nil")
	}
}

func TestDeployerBackup(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source.txt")
	backup := filepath.Join(tmpDir, "backup.txt")

	if err := os.WriteFile(source, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	d := NewDeployer()
	created, err := d.Backup(source, backup)
	if err != nil {
		t.Errorf("Backup() error = %v", err)
	}
	if !created {
		t.Error("Backup() created = false, want true")
	}

	content, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Backup content = %q, want %q", string(content), "test content")
	}
}

func TestDeployerBackupTargetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	backup := filepath.Join(tmpDir, "backup.txt")

	d := NewDeployer()
	created, err := d.Backup(filepath.Join(tmpDir, "nonexistent.txt"), backup)
	if err != nil {
		t.Errorf("Backup() error = %v, want nil for missing target", err)
	}
	if created {
		t.Error("Backup() created = true, want false for missing target")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("backup should not exist when target is missing, stat err = %v", err)
	}
}

func TestDeployerBackupCreatesDestinationDir(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source.txt")
	backup := filepath.Join(tmpDir, "nested", "deeper", "backup.txt")

	if err := os.WriteFile(source, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	d := NewDeployer()
	created, err := d.Backup(source, backup)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if !created {
		t.Error("Backup() created = false, want true")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup file not created: %v", err)
	}
}

func TestDeployerApply(t *testing.T) {
	tmpDir := t.TempDir()
	staged := filepath.Join(tmpDir, "staged.txt")
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(staged, []byte("new content"), 0644); err != nil {
		t.Fatalf("failed to write staged file: %v", err)
	}
	if err := os.WriteFile(target, []byte("old content"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}

	d := NewDeployer()
	err := d.Apply(staged, target)
	if err != nil {
		t.Errorf("Apply() error = %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(content) != "new content" {
		t.Errorf("Apply content = %q, want %q", string(content), "new content")
	}
}

func TestDeployerApplyStagedNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}

	d := NewDeployer()
	err := d.Apply(filepath.Join(tmpDir, "nonexistent.txt"), target)
	if err == nil {
		t.Error("Apply() expected error for nonexistent staged file, got nil")
	}
}

func TestDeployerRollback(t *testing.T) {
	tmpDir := t.TempDir()
	backup := filepath.Join(tmpDir, "backup.txt")
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(backup, []byte("restored content"), 0644); err != nil {
		t.Fatalf("failed to write backup file: %v", err)
	}
	if err := os.WriteFile(target, []byte("current content"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}

	d := NewDeployer()
	err := d.Rollback(backup, target)
	if err != nil {
		t.Errorf("Rollback() error = %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(content) != "restored content" {
		t.Errorf("Rollback content = %q, want %q", string(content), "restored content")
	}
}

func TestDeployerRollbackBackupNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to write target file: %v", err)
	}

	d := NewDeployer()
	err := d.Rollback(filepath.Join(tmpDir, "nonexistent.txt"), target)
	if err == nil {
		t.Error("Rollback() expected error for nonexistent backup, got nil")
	}
}

func TestDeployerBackupAndApply(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source.txt")
	backup := filepath.Join(tmpDir, "backup.txt")
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(source, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write target: %v", err)
	}

	d := NewDeployer()

	if _, err := d.Backup(source, backup); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if err := os.WriteFile(source, []byte("updated"), 0644); err != nil {
		t.Fatalf("failed to update source: %v", err)
	}

	if err := d.Apply(source, target); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	targetContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(targetContent) != "updated" {
		t.Errorf("target = %q, want %q", string(targetContent), "updated")
	}

	backupContent, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(backupContent) != "original" {
		t.Errorf("backup = %q, want %q", string(backupContent), "original")
	}
}

func TestDeployerRollbackAfterApply(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "source.txt")
	backup := filepath.Join(tmpDir, "backup.txt")
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(source, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write target: %v", err)
	}

	d := NewDeployer()

	if _, err := d.Backup(source, backup); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if err := os.WriteFile(source, []byte("new"), 0644); err != nil {
		t.Fatalf("failed to update source: %v", err)
	}

	if err := d.Apply(source, target); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := d.Rollback(backup, target); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("After rollback target = %q, want %q", string(content), "original")
	}
}

func TestDeployerBackupEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	source := filepath.Join(tmpDir, "empty.txt")
	backup := filepath.Join(tmpDir, "empty_backup.txt")

	if err := os.WriteFile(source, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	d := NewDeployer()
	created, err := d.Backup(source, backup)
	if err != nil {
		t.Errorf("Backup() on empty file error = %v", err)
	}
	if !created {
		t.Error("Backup() created = false, want true")
	}

	content, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("Backup content length = %d, want 0", len(content))
	}
}

func TestDeployerApplySamePath(t *testing.T) {
	tmpDir := t.TempDir()
	staged := filepath.Join(tmpDir, "staged.txt")
	target := filepath.Join(tmpDir, "target.txt")

	if err := os.WriteFile(staged, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write staged: %v", err)
	}
	if err := os.WriteFile(target, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write target: %v", err)
	}

	d := NewDeployer()
	err := d.Apply(staged, target)
	if err != nil {
		t.Errorf("Apply() same path error = %v", err)
	}
}

func TestDeployerApplyCreatesTargetDir(t *testing.T) {
	tmpDir := t.TempDir()
	staged := filepath.Join(tmpDir, "staged.txt")
	target := filepath.Join(tmpDir, "nested", "deeper", "target.txt")

	if err := os.WriteFile(staged, []byte("new content"), 0644); err != nil {
		t.Fatalf("failed to write staged: %v", err)
	}

	d := NewDeployer()
	if err := d.Apply(staged, target); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(content) != "new content" {
		t.Errorf("target = %q, want %q", string(content), "new content")
	}
}

func TestDeployerRollbackCreatesTargetDir(t *testing.T) {
	tmpDir := t.TempDir()
	backup := filepath.Join(tmpDir, "backup.txt")
	target := filepath.Join(tmpDir, "nested", "deeper", "target.txt")

	if err := os.WriteFile(backup, []byte("restored"), 0644); err != nil {
		t.Fatalf("failed to write backup: %v", err)
	}

	d := NewDeployer()
	if err := d.Rollback(backup, target); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(content) != "restored" {
		t.Errorf("target = %q, want %q", string(content), "restored")
	}
}

func TestDeployerRollbackNonexistentTarget(t *testing.T) {
	tmpDir := t.TempDir()
	backup := filepath.Join(tmpDir, "backup.txt")

	if err := os.WriteFile(backup, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write backup: %v", err)
	}

	d := NewDeployer()
	err := d.Rollback(backup, filepath.Join(tmpDir, "nonexistent.txt"))
	if err != nil {
		t.Errorf("Rollback() on nonexistent target error = %v (expected to work)", err)
	}
}
