package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/instancelock"
	"github.com/shaomian/PrismCat/internal/storage"
)

func uploadLegacyImport(t *testing.T, manager *Manager) ImportJob {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", "legacy-v2.4.1.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.UploadImport(context.Background(), file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current, err := manager.ImportStatus()
		if err != nil {
			t.Fatal(err)
		}
		if current != nil && current.State != "uploading" && current.State != "checking" {
			return *current
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("import %s did not finish", job.ID)
	return job
}

func TestWebImportSchedulesOfflineRestoreAndKeepsDestinationServer(t *testing.T) {
	source, repo, dir := backupFixture(t)
	source.Config.Update(func(c *config.Config) { c.Server.Port = 18999; c.Server.UIPassword = "destination-only-password" })
	if err := source.Config.Save(); err != nil {
		t.Fatal(err)
	}
	saveFixtureLog(t, repo, "destination-only", "")
	manager := NewManager(source, filepath.Join(dir, "backups"))
	t.Cleanup(manager.Close)
	job := uploadLegacyImport(t, manager)
	if job.State != "ready" || job.SourceVersion != "2.4.1" || job.Counts["request_logs"] != 1 {
		t.Fatalf("invalid preview: %+v", job)
	}
	if _, err := repo.GetLog("sample"); err == nil {
		t.Fatal("upload replaced live data")
	}
	if _, err := manager.ConfirmImport("wrong-id"); !errors.Is(err, ErrImportState) {
		t.Fatal("stale confirmation accepted", err)
	}
	job, err := manager.ConfirmImport(job.ID)
	if err != nil || job.State != "pending" {
		t.Fatal("confirmation failed", err)
	}
	if _, err = repo.GetLog("destination-only"); err != nil {
		t.Fatal("confirmation replaced live data", err)
	}
	lock, err := instancelock.Acquire(source.Config.FilePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyPendingImport(context.Background(), source.Config.FilePath()); err == nil {
		t.Fatal("startup import ignored running instance lock")
	}
	lock.Close()
	manager.Close()
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyPendingImport(context.Background(), source.Config.FilePath())
	if err != nil || result == nil || result.State != "completed" {
		t.Fatalf("startup import: %+v %v", result, err)
	}
	cfg, err := config.Load(source.Config.FilePath())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 18999 || cfg.Server.UIPassword != "destination-only-password" || !cfg.Storage.CleanupPaused {
		t.Fatal("destination server settings or cleanup protection lost")
	}
	restored, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restored.GetLog("sample"); err != nil {
		t.Fatal("imported log missing", err)
	}
	if _, err = restored.GetLog("destination-only"); err == nil {
		t.Fatal("import merged instead of replacing")
	}
	saveFixtureLog(t, restored, "after-import", "")
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	if repeated, err := ApplyPendingImport(context.Background(), source.Config.FilePath()); err != nil || repeated != nil {
		t.Fatal("completed import repeated", err)
	}
	restored, err = storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err = restored.GetLog("after-import"); err != nil {
		t.Fatal("subsequent startup wiped new data", err)
	}
	if _, err = os.Stat(importArchivePath(importDirectory(source.Config.FilePath()), job.ID)); !os.IsNotExist(err) {
		t.Fatal("uploaded archive not reclaimed")
	}
	if _, err = os.Stat(result.RollbackDirectory); err != nil {
		t.Fatal("rollback copy missing", err)
	}
}

func TestCancelImportAndUnsupportedLayout(t *testing.T) {
	source, repo, dir := backupFixture(t)
	manager := NewManager(source, filepath.Join(dir, "backups"))
	defer manager.Close()
	job := uploadLegacyImport(t, manager)
	if job.State != "ready" {
		t.Fatalf("check failed: %+v", job)
	}
	if _, err := manager.ConfirmImport(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.CancelImport(job.ID); err != nil {
		t.Fatal(err)
	}
	if current, err := manager.ImportStatus(); err != nil || current != nil {
		t.Fatal("cancelled import still scheduled", err)
	}
	if _, err := ApplyPendingImport(context.Background(), source.Config.FilePath()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetLog("sample"); err == nil {
		t.Fatal("cancelled import applied")
	}
	// A hard link is still a different installation path after replacement.
	_ = os.Link(source.Config.Storage.Database, filepath.Join(dir, "elsewhere.db"))
	source.Config.Update(func(c *config.Config) { c.Storage.Database = filepath.Join(dir, "elsewhere.db") })
	if manager.ImportSupported() {
		t.Fatal("nonstandard storage accepted")
	}
	if _, err := manager.UploadImport(context.Background(), io.NopCloser(strings.NewReader("zip")), 3); !errors.Is(err, ErrImportLayout) {
		t.Fatal("unsupported upload accepted", err)
	}
}

func TestImportRejectsInvalidArchivesSizeExpiryAndInterruptedChecks(t *testing.T) {
	source, _, dir := backupFixture(t)
	manager := NewManager(source, filepath.Join(dir, "backups"))
	defer manager.Close()
	if _, err := manager.UploadImport(context.Background(), io.NopCloser(strings.NewReader("x")), MaxImportBytes+1); err == nil {
		t.Fatal("oversized upload accepted")
	}
	job, err := manager.UploadImport(context.Background(), io.NopCloser(strings.NewReader("not a zip")), -1)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := manager.ImportStatus()
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "failed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	current, err := manager.ImportStatus()
	if err != nil || current.State != "failed" || current.Error != "backup_import_invalid" {
		t.Fatal("invalid import not rejected", err)
	}
	if _, err := manager.ConfirmImport(job.ID); err == nil {
		t.Fatal("invalid archive scheduled")
	}
	job = uploadLegacyImport(t, manager)
	manager.mu.Lock()
	manager.importJob.UploadedAt = time.Now().Add(-2 * importLifetime)
	manager.mu.Unlock()
	current, err = manager.ImportStatus()
	if err != nil || current.State != "failed" || current.Error != "backup_import_expired" {
		t.Fatal("expired preview accepted", err)
	}
	if _, err := os.Stat(importArchivePath(importDirectory(source.Config.FilePath()), job.ID)); !os.IsNotExist(err) {
		t.Fatal("expired ZIP leaked")
	}
}

type blockedUpload struct {
	once            sync.Once
	started, closed chan struct{}
}

func (b *blockedUpload) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.closed
	return 0, io.EOF
}
func (b *blockedUpload) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestManagerCloseCancelsBlockedImportUpload(t *testing.T) {
	source, _, dir := backupFixture(t)
	manager := NewManager(source, filepath.Join(dir, "backups"))
	body := &blockedUpload{started: make(chan struct{}), closed: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := manager.UploadImport(context.Background(), body, -1); done <- err }()
	<-body.started
	closed := make(chan struct{})
	go func() { manager.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close hung on upload")
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled upload succeeded")
	}
}

func TestInterruptedImportCheckIsNotReplayedAtStartup(t *testing.T) {
	source, repo, dir := backupFixture(t)
	manager := NewManager(source, filepath.Join(dir, "backups"))
	job := uploadLegacyImport(t, manager)
	manager.Close()
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	record, err := readImport(importDirectory(source.Config.FilePath()))
	if err != nil {
		t.Fatal(err)
	}
	record.State = "checking"
	if err = writeJSON(importStatePath(importDirectory(source.Config.FilePath())), record); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyPendingImport(context.Background(), source.Config.FilePath())
	if err != nil || result == nil || result.State != "failed" || result.Error != "backup_import_interrupted" {
		t.Fatal("interrupted import replayed", err)
	}
	if _, err := os.Stat(importArchivePath(importDirectory(source.Config.FilePath()), job.ID)); !os.IsNotExist(err) {
		t.Fatal("interrupted ZIP leaked")
	}
}

func TestChangedPendingArchiveDoesNotReplaceOriginalData(t *testing.T) {
	source, repo, dir := backupFixture(t)
	saveFixtureLog(t, repo, "original", "")
	manager := NewManager(source, filepath.Join(dir, "backups"))
	job := uploadLegacyImport(t, manager)
	if _, err := manager.ConfirmImport(job.ID); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(importArchivePath(importDirectory(source.Config.FilePath()), job.ID), []byte("changed after confirmation"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyPendingImport(context.Background(), source.Config.FilePath())
	if err != nil || result == nil || result.State != "failed" {
		t.Fatal("changed import applied", err)
	}
	restored, err := storage.NewSQLiteRepository(source.Config.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err = restored.GetLog("original"); err != nil {
		t.Fatal("failed import lost original data", err)
	}
	if _, err = restored.GetLog("sample"); err == nil {
		t.Fatal("failed import replaced data")
	}
	if repeated, err := ApplyPendingImport(context.Background(), source.Config.FilePath()); err != nil || repeated != nil {
		t.Fatal("failed import repeated", err)
	}
}

func TestInterruptedImportInstallationRollsBackBeforeStartup(t *testing.T) {
	target := t.TempDir()
	id := uuid.NewString()
	stage, saved := filepath.Join(target, ".prismcat-install-"+id), filepath.Join(target, ".prismcat-rollback-"+id)
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(saved, 0700); err != nil {
		t.Fatal(err)
	}
	journal := restoreJournal{ID: id}
	for _, name := range []string{"prismcat.db", "prismcat.db-wal", "prismcat.db-shm", "prismcat.db-journal", "blobs", "config.yaml"} {
		managed := name == "prismcat.db" || name == "blobs" || name == "config.yaml"
		journal.Entries = append(journal.Entries, restoreEntry{Name: name, HadOriginal: managed, Install: managed})
		if !managed {
			continue
		}
		if name == "blobs" {
			if err := os.Mkdir(filepath.Join(target, name), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(stage, name), 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(filepath.Join(target, name), []byte("old-"+name), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, name), []byte("new-"+name), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writeJSON(filepath.Join(saved, "journal.json"), journal); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(target, journalName), journal); err != nil {
		t.Fatal(err)
	}
	if err := install(context.Background(), target, stage, saved, journal, func(step int) error {
		if step == 2 {
			return errors.New("power loss")
		}
		return nil
	}); err == nil {
		t.Fatal("missing failure")
	}
	configPath := filepath.Join(target, "config.yaml")
	dir := importDirectory(configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(importStatePath(dir), importRecord{ImportJob: ImportJob{ID: uuid.NewString(), State: "applying"}}); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyPendingImport(context.Background(), configPath)
	if err != nil || result == nil || result.State != "failed" || PendingRestore(configPath) {
		t.Fatal("interrupted installation not recovered", err)
	}
	for _, name := range []string{"prismcat.db", "config.yaml"} {
		data, err := os.ReadFile(filepath.Join(target, name))
		if err != nil || string(data) != "old-"+name {
			t.Fatal("rollback lost original file", name, err)
		}
	}
}
