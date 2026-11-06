package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/instancelock"
	"github.com/shaomian/PrismCat/internal/storage"
)

func backupFixture(t *testing.T) (Source, *storage.SQLiteRepository, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{Storage: config.StorageConfig{Database: filepath.Join(dir, "prismcat.db"), BlobDir: filepath.Join(dir, "blobs"), BlobStore: "fs", RetentionDays: 7}, Server: config.ServerConfig{Port: 8711, UIPassword: "stored-secret"}, Analysis: config.AnalysisConfig{APIKey: "model-secret"}}
	data, err := cfg.BackupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "config.yaml")
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	blobs, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
	if err != nil {
		t.Fatal(err)
	}
	return Source{Snapshot: repo.Snapshot, Blobs: blobs, Config: cfg}, repo, dir
}

func saveFixtureLog(t *testing.T, repo *storage.SQLiteRepository, id, ref string) {
	t.Helper()
	if err := repo.SaveLog(&storage.RequestLog{ID: id, TraceID: "trace-" + id, CreatedAt: time.Now().Add(-30 * 24 * time.Hour), Upstream: "a", Method: "POST", Path: "/v1/responses", StatusCode: 200, RequestBody: "preview", RequestBodyRef: ref, ResponseBody: "event: response.completed\ndata: {}\n\n", Streaming: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteBackupRestoreAndRollback(t *testing.T) {
	ctx := context.Background()
	source, repo, dir := backupFixture(t)
	body := bytes.Repeat([]byte{0, 1, 2, 250, 255}, 512*1024)
	ref, err := source.Blobs.Put(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	saveFixtureLog(t, repo, "sample", ref)
	if _, err = repo.SaveLogAnnotation("sample", storage.LogAnnotation{Saved: true, Status: "todo", Labels: []string{"backup"}, Note: "preserve me"}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveTraceMetadata(storage.TraceMetadata{TraceID: "trace-sample", Name: "original task", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	experiment, err := repo.SaveExperiment(benchmark.Experiment{Name: "comparison", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.SaveExperimentRun(benchmark.Run{ExperimentID: experiment.ID, Name: "run", Conditions: benchmark.Conditions{Agent: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	run, err = repo.StartExperimentCapture(run.ID, run.Revision, "a")
	if err != nil {
		t.Fatal(err)
	}
	reviewCfg := benchmark.ReviewConfig{TemplateName: "template", Points: []benchmark.ReviewPoint{{ID: "logic", Name: "Logic", Instructions: "Cite evidence"}}, Selected: []string{"logic"}}
	template, err := repo.SaveReviewTemplate(benchmark.ReviewTemplate{Name: "template", Config: reviewCfg})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateReviewReport(benchmark.ReviewReport{ID: "report", ExperimentID: experiment.ID, Status: "running", CreatedAt: time.Now(), Config: reviewCfg, Tasks: []benchmark.ReviewTask{{RunID: run.ID, Metrics: benchmark.Metrics{LogIDs: []string{"sample"}}}}}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "backup.zip")
	manifest, err := Create(ctx, source, archive, uuid.NewString(), "online", nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["request_logs"] != 1 || manifest.Counts["review_reports"] != 1 {
		t.Fatalf("incomplete manifest: %v", manifest.Counts)
	}
	validated, err := Validate(ctx, archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	validated.Close()
	target := filepath.Join(dir, "restored")
	if _, err = Restore(ctx, archive, RestoreOptions{Target: target, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(target, "prismcat.db")); !os.IsNotExist(err) {
		t.Fatal("dry run installed data")
	}
	result, err := Restore(ctx, archive, RestoreOptions{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(result.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Storage.CleanupPaused || cfg.Analysis.APIKey != "model-secret" || cfg.Server.UIPassword != "stored-secret" {
		t.Fatal("configuration not restored")
	}
	restored, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := restored.GetLog("sample")
	if err != nil || entry.RequestBodyRef != ref || !entry.Annotation.Saved || entry.Annotation.Note != "preserve me" {
		t.Fatalf("log not restored: %+v %v", entry, err)
	}
	store, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := store.Get(ctx, ref)
	if err != nil || !bytes.Equal(actual, body) {
		t.Fatal("raw binary body not restored")
	}
	metadata, err := restored.GetTraceMetadataDocument("trace-sample")
	if err != nil || metadata.Name != "original task" {
		t.Fatal("task metadata lost")
	}
	if _, err = restored.GetReviewTemplate(template.ID); err != nil {
		t.Fatal("template lost", err)
	}
	report, err := restored.GetReviewReport("report")
	if err != nil || report.Status != "interrupted" {
		t.Fatal("active report not interrupted", err)
	}
	if len(report.Tasks) != 1 || len(report.Tasks[0].Metrics.LogIDs) != 1 || report.Tasks[0].Metrics.LogIDs[0] != "sample" {
		t.Fatal("review evidence membership lost")
	}
	loadedRun, err := restored.GetExperimentRun(run.ID)
	if err != nil || loadedRun.State != "completed" || loadedRun.FinishedAt == nil || restored.CaptureRunForUpstream("a") != "" {
		t.Fatal("active capture not finalized", err)
	}
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	sourceReport, err := repo.GetReviewReport("report")
	if err != nil || sourceReport.Status != "running" {
		t.Fatal("source database mutated")
	}
	if _, err = Restore(ctx, archive, RestoreOptions{Target: target}); err == nil {
		t.Fatal("replacement without flag allowed")
	}
	// Replace a different destination and restore the exact old set using rollback.
	old, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	saveFixtureLog(t, old, "destination-only", "")
	_ = old.Close()
	result, err = Restore(ctx, archive, RestoreOptions{Target: target, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	// A damaged rollback copy must be rejected before touching the destination.
	savedConfig := filepath.Join(result.RollbackDirectory, "config.yaml")
	if err = os.Rename(savedConfig, savedConfig+".missing"); err != nil {
		t.Fatal(err)
	}
	if err = Rollback(target, result.RollbackDirectory); err == nil || PendingRestore(result.ConfigPath) {
		t.Fatal("incomplete rollback was applied")
	}
	if err = os.Rename(savedConfig+".missing", savedConfig); err != nil {
		t.Fatal(err)
	}
	if err = Rollback(target, result.RollbackDirectory); err != nil {
		t.Fatal(err)
	}
	old, err = storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err = old.GetLog("destination-only"); err != nil {
		t.Fatal("rollback lost original destination", err)
	}
	if PendingRestore(result.ConfigPath) {
		t.Fatal("journal not cleared")
	}
}

func TestOnlineBackupUsesSnapshotRefsWhileSourceChanges(t *testing.T) {
	ctx := context.Background()
	source, repo, dir := backupFixture(t)
	oldRef, err := source.Blobs.Put(ctx, []byte("old body"))
	if err != nil {
		t.Fatal(err)
	}
	newRef, err := source.Blobs.Put(ctx, []byte("new body"))
	if err != nil {
		t.Fatal(err)
	}
	saveFixtureLog(t, repo, "old", oldRef)
	originalSnapshot := source.Snapshot
	source.Snapshot = func(ctx context.Context, name string) error {
		if err := originalSnapshot(ctx, name); err != nil {
			return err
		}
		saveFixtureLog(t, repo, "new", newRef)
		if _, err := repo.DeleteLogsBefore(time.Now().Add(-time.Hour)); err != nil {
			return err
		}
		return nil
	}
	archive := filepath.Join(dir, "online.zip")
	if _, err = Create(ctx, source, archive, uuid.NewString(), "online", nil); err != nil {
		t.Fatal(err)
	}
	v, err := Validate(ctx, archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if v.Manifest.Counts["request_logs"] != 1 {
		t.Fatal("snapshot not stable")
	}
	name, _ := storage.BackupBlobPath(oldRef)
	if body, err := os.ReadFile(filepath.Join(v.Directory, filepath.FromSlash(name))); err != nil || string(body) != "old body" {
		t.Fatal("source deletion changed snapshot body set")
	}
}

func TestBackupDestinationCannotParticipateInBlobStorageAccounting(t *testing.T) {
	source, _, dir := backupFixture(t)
	_, err := Create(context.Background(), source, filepath.Join(dir, "blobs", "backup.zip"), uuid.NewString(), "offline", nil)
	if err == nil || !strings.Contains(err.Error(), "outside body storage") {
		t.Fatal("backup was allowed in body storage", err)
	}
}

func TestRestoreInterruptsPersistedInFlightRequestsWithoutChangingSource(t *testing.T) {
	source, repo, dir := backupFixture(t)
	if err := repo.SaveLog(&storage.RequestLog{ID: "pending", CreatedAt: time.Now(), Upstream: "a", Method: "POST", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "pending.zip")
	if _, err := Create(context.Background(), source, archive, uuid.NewString(), "online", nil); err != nil {
		t.Fatal(err)
	}
	result, err := Restore(context.Background(), archive, RestoreOptions{Target: filepath.Join(dir, "target")})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(result.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	entry, err := restored.GetLog("pending")
	if err != nil || entry.Error != "request interrupted by restore" {
		t.Fatal("in-flight snapshot still appears active", err)
	}
	entry, err = repo.GetLog("pending")
	if err != nil || entry.Error != "" {
		t.Fatal("source request modified", err)
	}
}

func TestBackupRejectsMissingAndCorruptBodiesAndCleansTemporaryFiles(t *testing.T) {
	ctx := context.Background()
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			source, repo, dir := backupFixture(t)
			ref, err := source.Blobs.Put(ctx, []byte("body"))
			if err != nil {
				t.Fatal(err)
			}
			saveFixtureLog(t, repo, "log", ref)
			name, _ := storage.BackupBlobPath(ref)
			blobPath := filepath.Join(dir, filepath.FromSlash(name))
			if corrupt {
				err = os.WriteFile(blobPath, []byte("changed"), 0600)
			} else {
				err = os.Remove(blobPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "backup.zip")
			if _, err = Create(ctx, source, out, uuid.NewString(), "online", nil); err == nil {
				t.Fatal("incomplete backup succeeded")
			}
			if _, err = os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("partial archive published")
			}
			matches, _ := filepath.Glob(filepath.Join(dir, ".prismcat-backup-*"))
			if len(matches) != 0 {
				t.Fatal("temporary data leaked")
			}
		})
	}
}

func rewriteArchive(t *testing.T, original, destination string, mutate func(string, []byte) (string, []byte), extra string) {
	t.Helper()
	reader, err := zip.OpenReader(original)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for _, entry := range reader.File {
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		name, data := mutate(entry.Name, data)
		if name == "" {
			continue
		}
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != "" {
		w, err := writer.Create(extra)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("bad"))
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveValidationRejectsTamperingAndUnsafePaths(t *testing.T) {
	source, repo, dir := backupFixture(t)
	saveFixtureLog(t, repo, "log", "")
	original := filepath.Join(dir, "original.zip")
	if _, err := Create(context.Background(), source, original, uuid.NewString(), "offline", nil); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"checksum", "version", "missing", "duplicate", "traversal", "absolute", "backslash"} {
		t.Run(kind, func(t *testing.T) {
			name := filepath.Join(dir, kind+".zip")
			extra := ""
			switch kind {
			case "duplicate":
				extra = "config/source.yaml"
			case "traversal":
				extra = "../escape"
			case "absolute":
				extra = "/tmp/escape"
			case "backslash":
				extra = `blobs\..\escape`
			}
			rewriteArchive(t, original, name, func(path string, data []byte) (string, []byte) {
				if kind == "checksum" && path == "config/source.yaml" {
					data = append(data, ' ')
				}
				if kind == "missing" && path == "database/prismcat.db" {
					return "", nil
				}
				if kind == "version" && path == "manifest.json" {
					var m Manifest
					_ = json.Unmarshal(data, &m)
					m.AppVersion = "future-version"
					data, _ = json.Marshal(m)
				}
				return path, data
			}, extra)
			if value, err := Validate(context.Background(), name, dir); err == nil {
				value.Close()
				t.Fatal("invalid archive accepted")
			}
			matches, _ := filepath.Glob(filepath.Join(dir, ".prismcat-validate-*"))
			if len(matches) != 0 {
				t.Fatal("validation leaked temporary files")
			}
		})
	}
}

func TestOfflineRestoreHonorsInstanceLock(t *testing.T) {
	source, repo, dir := backupFixture(t)
	saveFixtureLog(t, repo, "log", "")
	archive := filepath.Join(dir, "backup.zip")
	if _, err := Create(context.Background(), source, archive, uuid.NewString(), "online", nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := instancelock.Acquire(filepath.Join(target, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := Restore(context.Background(), archive, RestoreOptions{Target: target}); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatal("restore ignored live instance", err)
	}
}
