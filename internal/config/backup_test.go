package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupConfigExcludesEnvironmentPasswordAndParseHasNoSideEffects(t *testing.T) {
	t.Setenv("PRISMCAT_UI_PASSWORD", "environment-secret")
	t.Setenv("PRISMCAT_PORT", "9999")
	dir := t.TempDir()
	name := filepath.Join(dir, "config.yaml")
	data := []byte("server:\n  port: 8711\n  ui_password: file-secret\nstorage:\n  database: " + filepath.Join(dir, "live", "db") + "\n  blob_dir: " + filepath.Join(dir, "live", "blobs") + "\n")
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Server.Port != 8711 || parsed.Server.UIPassword != "file-secret" {
		t.Fatal("parse applied environment")
	}
	if _, err = os.Stat(filepath.Join(dir, "live")); !os.IsNotExist(err) {
		t.Fatal("parse created directories")
	}
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.UIPassword != "environment-secret" {
		t.Fatal("live environment not applied")
	}
	backup, err := cfg.BackupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(backup), "environment-secret") || !strings.Contains(string(backup), "file-secret") {
		t.Fatal("backup leaked environment secret or lost stored password")
	}
	parsed, err = Parse(backup)
	if err != nil || parsed.Server.Port != 9999 {
		t.Fatal("effective non-secret settings lost", err)
	}
}

func TestResumeCleanupPersistsAndRollsBackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(name, []byte("storage:\n  cleanup_paused: true\n  database: "+filepath.Join(dir, "db")+"\n  blob_dir: "+filepath.Join(dir, "blobs")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ResumeCleanup(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(name)
	if err != nil || reloaded.Storage.CleanupPaused {
		t.Fatal("resume not persisted", err)
	}
	cfg.Update(func(c *Config) { c.Storage.CleanupPaused = true })
	cfg.configPath = filepath.Join(dir, "missing", "config.yaml")
	if err = cfg.ResumeCleanup(); err == nil || !cfg.StorageSnapshot().CleanupPaused {
		t.Fatal("failed save disabled protection")
	}
}
