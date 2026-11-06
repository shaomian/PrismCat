package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestAppVersionCompatibility(t *testing.T) {
	for _, tt := range []struct {
		source, target string
		allowed        bool
	}{
		{"2.4.1", "2.4.1", true},
		{"v2.4.1", "2.4.1", true},
		{"2.4.1", "v2.4.2", true},
		{"2.4.1", "2.10.0", true},
		{"2.4.1", "2.4.0", false},
		{"2.4.1", "3.0.0", false},
		{"2.4.2", "2.4.1", false},
		{"2.4.0", "2.4.1", false},
		{"2.3.0", "2.4.1", false},
		{"2.4.2", "2.4.3", false},
		{"2.4.2", "2.4.2", true},
		{"dev", "dev", true},
		{"vdev", "dev", false},
		{"2.4.2-rc.1", "2.4.2-rc.1", true},
		{"2.4.2-rc.1", "2.4.2", false},
		{"2.4.1", "2.4.2-rc.1", false},
		{"2.4.1", "dev", false},
		{"2.4.1", "2.04.2", false},
		{"2.4.1", "2.4", false},
		{"2.4.1", "2.4.18446744073709551616", false},
		{"", "", false},
	} {
		t.Run(tt.source+"_to_"+tt.target, func(t *testing.T) {
			if err := checkAppVersion(tt.source, tt.target); (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v: %v", tt.allowed, err)
			}
		})
	}
}

func TestFrozen241ArchiveRestoresOnNewerRelease(t *testing.T) {
	archive := filepath.Join("testdata", "legacy-v2.4.1.zip")
	original, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	if hex.EncodeToString(hash[:]) != "88041ea0b11f1caf9fe8bb6ae74b76d99a9b441b1144e4fcb8443f8329329345" {
		t.Fatal("frozen pre-compatibility archive changed")
	}
	// The current release exercises the actual migration code on every future
	// build; a next-release label also exercises cross-version checks today.
	for _, version := range []string{config.Version, "2.4.2"} {
		t.Run(version, func(t *testing.T) {
			previous := config.Version
			config.Version = version
			t.Cleanup(func() { config.Version = previous })
			ctx := context.Background()
			target := t.TempDir()
			preview, err := Restore(ctx, archive, RestoreOptions{Target: target, DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			if preview.Manifest.AppVersion != "2.4.1" || preview.TargetAppVersion != version || preview.UpgradeRequired != (normalizedVersion(version) != "2.4.1") {
				t.Fatalf("incorrect compatibility preview: %+v", preview)
			}
			for _, name := range []string{"prismcat.db", "config.yaml", "blobs"} {
				if _, err = os.Stat(filepath.Join(target, name)); !os.IsNotExist(err) {
					t.Fatalf("dry run installed %s", name)
				}
			}
			result, err := Restore(ctx, archive, RestoreOptions{Target: target})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(result.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !cfg.Storage.CleanupPaused || cfg.Storage.RetentionDays != 7 || cfg.Server.UIPassword != "stored-secret" || cfg.Analysis.APIKey != "model-secret" {
				t.Fatal("legacy configuration or cleanup protection lost")
			}
			repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repo.Close() })
			entry, err := repo.GetLog("sample")
			if err != nil || !entry.Streaming || entry.ResponseBody != "event: response.completed\ndata: {}\n\n" || !entry.Annotation.Saved || entry.Annotation.Note != "preserve me" {
				t.Fatal("legacy SSE log or annotation lost", err)
			}
			blobs, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
			if err != nil {
				t.Fatal(err)
			}
			body, err := blobs.Get(ctx, entry.RequestBodyRef)
			if err != nil || !bytes.Equal(body, bytes.Repeat([]byte{0, 1, 2, 250, 255}, 512*1024)) {
				t.Fatal("legacy raw binary body changed", err)
			}
			metadata, err := repo.GetTraceMetadataDocument("trace-sample")
			if err != nil || metadata.Name != "original task" || !metadata.Pinned {
				t.Fatal("legacy Trace metadata lost", err)
			}
			report, err := repo.GetReviewReport("report")
			if err != nil || report.Status != "interrupted" || len(report.Tasks) != 1 || len(report.Tasks[0].Metrics.LogIDs) != 1 || report.Tasks[0].Metrics.LogIDs[0] != "sample" {
				t.Fatal("legacy review evidence lost or report not interrupted", err)
			}
			experiment, err := repo.GetExperiment(report.ExperimentID)
			if err != nil || experiment.Name != "comparison" || !experiment.Keep {
				t.Fatal("legacy experiment or retention lost", err)
			}
			run, err := repo.GetExperimentRun(report.Tasks[0].RunID)
			if err != nil || run.State != "completed" || run.FinishedAt == nil || repo.CaptureRunForUpstream("a") != "" {
				t.Fatal("legacy capture not finalized", err)
			}
			templates, total, err := repo.ListReviewTemplates("", 0, 10)
			if err != nil || total != 1 || len(templates) != 1 || templates[0].Name != "template" {
				t.Fatal("legacy review template lost", err)
			}
			if err = repo.Close(); err != nil {
				t.Fatal(err)
			}
			counts, _, _, err := inspectDatabase(ctx, cfg.Storage.Database)
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"request_logs", "log_annotations", "trace_metadata", "experiments", "experiment_runs", "capture_runs", "review_templates", "review_reports", "review_card_logs"} {
				if counts[table] != preview.Manifest.Counts[table] {
					t.Fatalf("legacy %s count changed", table)
				}
			}
		})
	}
	unchanged, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(original, unchanged) {
		t.Fatal("restoring modified the source archive", err)
	}
}

func TestIncompatibleArchiveLeavesDestinationUntouched(t *testing.T) {
	for _, tt := range []struct {
		name, source, target string
		format               int
	}{
		{"newer", "2.4.3", "2.4.2", 1},
		{"unverified", "2.3.0", "2.4.2", 1},
		{"downgrade", "2.4.1", "2.4.0", 1},
		{"prerelease", "2.4.2-rc.1", "2.4.2", 1},
		{"format", "2.4.1", "2.4.2", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			previous := config.Version
			config.Version = tt.target
			t.Cleanup(func() { config.Version = previous })
			source, repo, target := backupFixture(t)
			saveFixtureLog(t, repo, "destination-only", "")
			if err := repo.Close(); err != nil {
				t.Fatal(err)
			}
			beforeDB, err := os.ReadFile(source.Config.Storage.Database)
			if err != nil {
				t.Fatal(err)
			}
			beforeConfig, err := os.ReadFile(source.Config.FilePath())
			if err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "incompatible.zip")
			rewriteArchive(t, filepath.Join("testdata", "legacy-v2.4.1.zip"), archive, func(name string, data []byte) (string, []byte) {
				if name == "manifest.json" {
					var manifest Manifest
					if err := json.Unmarshal(data, &manifest); err != nil {
						t.Fatal(err)
					}
					manifest.AppVersion, manifest.FormatVersion = tt.source, tt.format
					data, _ = json.Marshal(manifest)
				}
				return name, data
			}, "")
			for _, dryRun := range []bool{true, false} {
				if _, err = Restore(context.Background(), archive, RestoreOptions{Target: target, Replace: true, DryRun: dryRun}); err == nil {
					t.Fatal("incompatible restore accepted")
				}
			}
			afterDB, err := os.ReadFile(source.Config.Storage.Database)
			if err != nil || !bytes.Equal(beforeDB, afterDB) {
				t.Fatal("rejected restore changed database", err)
			}
			afterConfig, err := os.ReadFile(source.Config.FilePath())
			if err != nil || !bytes.Equal(beforeConfig, afterConfig) || PendingRestore(source.Config.FilePath()) {
				t.Fatal("rejected restore changed configuration or created journal", err)
			}
		})
	}
}

func TestLegacyUpgradeStillChecksBodyHashes(t *testing.T) {
	previous := config.Version
	config.Version = "2.4.2"
	t.Cleanup(func() { config.Version = previous })
	dir := t.TempDir()
	archive := filepath.Join(dir, "corrupt.zip")
	rewriteArchive(t, filepath.Join("testdata", "legacy-v2.4.1.zip"), archive, func(name string, data []byte) (string, []byte) {
		if strings.HasPrefix(name, "blobs/") {
			data[0] ^= 1 // Same length, so only the content checksum detects this.
		}
		return name, data
	}, "")
	if value, err := Validate(context.Background(), archive, dir); err == nil {
		value.Close()
		t.Fatal("corrupt legacy body accepted")
	} else if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatal("body corruption was not checked", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".prismcat-validate-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("failed legacy validation leaked staging data", err)
	}
}

func TestLegacyMigrationFailureDoesNotReplaceDestination(t *testing.T) {
	previous := config.Version
	config.Version = "2.4.2"
	t.Cleanup(func() { config.Version = previous })
	ctx := context.Background()
	dir := t.TempDir()
	validated, err := Validate(ctx, filepath.Join("testdata", "legacy-v2.4.1.zip"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer validated.Close()
	filename := filepath.Join(validated.Directory, "database", "prismcat.db")
	db, err := storage.OpenBackupDatabase(filename, false)
	if err != nil {
		t.Fatal(err)
	}
	// The package remains self-consistent and readable, but state recovery fails.
	_, err = db.Exec(`CREATE TRIGGER fail_recovery BEFORE UPDATE ON capture_runs BEGIN SELECT RAISE(ABORT,'injected recovery failure'); END`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	counts, _, schema, err := inspectDatabase(ctx, filename)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	manifest := validated.Manifest
	manifest.Counts, manifest.SchemaHash = counts, schema
	hash := sha256.Sum256(data)
	for i := range manifest.Files {
		if manifest.Files[i].Path == "database/prismcat.db" {
			manifest.Files[i].Size = int64(len(data))
			manifest.Files[i].SHA256 = hex.EncodeToString(hash[:])
		}
	}
	metadata, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "migration-failure.zip")
	rewriteArchive(t, filepath.Join("testdata", "legacy-v2.4.1.zip"), archive, func(name string, original []byte) (string, []byte) {
		switch name {
		case "database/prismcat.db":
			return name, data
		case "manifest.json":
			return name, metadata
		}
		return name, original
	}, "")
	target := t.TempDir()
	installed, err := Restore(ctx, filepath.Join("testdata", "legacy-v2.4.1.zip"), RestoreOptions{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(installed.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	saveFixtureLog(t, repo, "destination-only", "")
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}
	beforeDB, err := os.ReadFile(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := os.ReadFile(cfg.FilePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		if _, err = Restore(ctx, archive, RestoreOptions{Target: target, Replace: true, DryRun: dryRun}); err == nil || !strings.Contains(err.Error(), "injected recovery failure") {
			t.Fatal("expected migration failure", err)
		}
	}
	afterDB, err := os.ReadFile(cfg.Storage.Database)
	if err != nil || !bytes.Equal(beforeDB, afterDB) {
		t.Fatal("migration failure replaced destination database", err)
	}
	afterConfig, err := os.ReadFile(cfg.FilePath())
	if err != nil || !bytes.Equal(beforeConfig, afterConfig) || PendingRestore(cfg.FilePath()) {
		t.Fatal("migration failure changed destination configuration", err)
	}
}
