package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/backup"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/instancelock"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestBackupCommandsRoundTripWithoutStartingServer(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Storage: config.StorageConfig{Database: filepath.Join(dir, "db"), BlobDir: filepath.Join(dir, "blobs"), BlobStore: "fs"}}
	data, err := cfg.BackupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "config.yaml")
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveLog(&storage.RequestLog{ID: "original", CreatedAt: time.Now(), Upstream: "a", Method: "POST", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "backup.zip")
	var output bytes.Buffer
	lock, err := instancelock.Acquire(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = runBackup([]string{"create", "--config", name, "--file", archive}, &output); err == nil {
		t.Fatal("offline create accepted running instance")
	}
	lock.Close()
	if err = runBackup([]string{"create", "--config", name, "--file", archive}, &output); err != nil {
		t.Fatal(err)
	}
	if err = runBackup([]string{"inspect", "--file", archive}, &output); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err = runBackup([]string{"restore", "--file", archive, "--target", target, "--dry-run"}, &output); err != nil {
		t.Fatal(err)
	}
	if err = runBackup([]string{"restore", "--file", archive, "--target", target}, &output); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(filepath.Join(target, "config.yaml"))
	if err != nil || !loaded.Storage.CleanupPaused {
		t.Fatal("restored config not protected", err)
	}
	repo, err = storage.NewSQLiteRepository(loaded.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if _, err = repo.GetLog("original"); err != nil {
		t.Fatal("CLI round trip lost data", err)
	}
}

func TestBackupCommandsRestoreFrozenLegacyArchiveOnNewerVersion(t *testing.T) {
	previous := config.Version
	config.Version = "2.4.2"
	t.Cleanup(func() { config.Version = previous })
	archive := filepath.Join("..", "..", "internal", "backup", "testdata", "legacy-v2.4.1.zip")
	var output bytes.Buffer
	if err := runBackup([]string{"inspect", "--file", archive}, &output); err != nil {
		t.Fatal(err)
	}
	var manifest backup.Manifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil || manifest.AppVersion != "2.4.1" {
		t.Fatal("inspect lost the original source version", err)
	}
	output.Reset()
	target := t.TempDir()
	if err := runBackup([]string{"restore", "--file", archive, "--target", target, "--dry-run"}, &output); err != nil {
		t.Fatal(err)
	}
	var preview backup.RestoreResult
	if err := json.Unmarshal(output.Bytes(), &preview); err != nil || !preview.DryRun || !preview.UpgradeRequired || preview.TargetAppVersion != "2.4.2" {
		t.Fatal("CLI preview did not identify the upgrade", err)
	}
	output.Reset()
	if err := runBackup([]string{"restore", "--file", archive, "--target", target}, &output); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(target, "config.yaml"))
	if err != nil || !cfg.Storage.CleanupPaused {
		t.Fatal("legacy CLI restore lost protection", err)
	}
	repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if entry, err := repo.GetLog("sample"); err != nil || entry.Annotation.Note != "preserve me" {
		t.Fatal("CLI upgrade lost legacy log data", err)
	}
}
