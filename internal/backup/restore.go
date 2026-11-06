package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/instancelock"
	"github.com/shaomian/PrismCat/internal/storage"
)

const journalName = ".prismcat-restore.json"

type RestoreOptions struct {
	Target  string
	Replace bool
	DryRun  bool
	// KeepServerConfig retains only the destination's server settings. Business
	// routing/rules remain those from the archive. Environment overrides still apply at startup.
	KeepServerConfig string
}

type RestoreResult struct {
	Manifest                   Manifest             `json:"manifest"`
	TargetAppVersion           string               `json:"target_app_version"`
	UpgradeRequired            bool                 `json:"upgrade_required"`
	Target                     string               `json:"target"`
	ConfigPath                 string               `json:"config_path"`
	RollbackDirectory          string               `json:"rollback_directory,omitempty"`
	CleanupPaused              bool                 `json:"cleanup_paused"`
	DryRun                     bool                 `json:"dry_run"`
	Server                     RestoreServerPreview `json:"server"`
	EnvironmentOverrides       []string             `json:"environment_overrides"`
	BackupEnvironmentVariables []string             `json:"backup_environment_variables"`
}

type RestoreServerPreview struct {
	Address      string   `json:"address"`
	Port         int      `json:"port"`
	UIHosts      []string `json:"ui_hosts"`
	ProxyDomains []string `json:"proxy_domains"`
	HasPassword  bool     `json:"has_password"`
}

type restoreEntry struct {
	Name        string `json:"name"`
	HadOriginal bool   `json:"had_original"`
	Install     bool   `json:"install"`
}
type restoreJournal struct {
	ID      string         `json:"id"`
	Entries []restoreEntry `json:"entries"`
}

func PendingRestore(configPath string) bool {
	_, err := os.Lstat(filepath.Join(filepath.Dir(configPath), journalName))
	return !os.IsNotExist(err)
}

// Restore always installs a portable layout inside Target, including when Target
// is a mounted Docker volume. It never renames the volume itself.
func Restore(ctx context.Context, archive string, options RestoreOptions) (result RestoreResult, err error) {
	target, err := filepath.Abs(options.Target)
	if err != nil || options.Target == "" {
		return result, errors.New("restore target required")
	}
	if err = os.MkdirAll(target, 0700); err != nil {
		return result, err
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return result, err
	}
	target = resolved
	lock, err := instancelock.Acquire(filepath.Join(target, "config.yaml"), filepath.Join(target, "prismcat.db"), filepath.Join(target, "blobs"))
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if PendingRestore(filepath.Join(target, "config.yaml")) {
		return result, errors.New("unfinished restore: run backup rollback first")
	}
	validated, err := Validate(ctx, archive, target)
	if err != nil {
		return result, err
	}
	defer validated.Close()
	result = RestoreResult{
		Manifest: validated.Manifest, TargetAppVersion: config.Version,
		UpgradeRequired: normalizedVersion(validated.Manifest.AppVersion) != normalizedVersion(config.Version),
		Target:          target, ConfigPath: filepath.Join(target, "config.yaml"), CleanupPaused: true, DryRun: options.DryRun,
	}
	data, err := os.ReadFile(filepath.Join(validated.Directory, "config", "source.yaml"))
	if err != nil {
		return result, err
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return result, err
	}
	if options.KeepServerConfig != "" {
		serverData, readErr := os.ReadFile(options.KeepServerConfig)
		if readErr != nil {
			return result, readErr
		}
		serverCfg, parseErr := config.Parse(serverData)
		if parseErr != nil {
			return result, parseErr
		}
		cfg.Server = serverCfg.Server
	}
	cfg.Storage.Database = filepath.Join(target, "prismcat.db")
	cfg.Storage.BlobDir = filepath.Join(target, "blobs")
	cfg.Storage.BlobStore = "fs"
	cfg.Storage.CleanupPaused = true
	result.Server = RestoreServerPreview{Address: cfg.Server.Addr, Port: cfg.Server.Port, UIHosts: cfg.Server.UIHosts, ProxyDomains: cfg.Server.ProxyDomains, HasPassword: cfg.Server.UIPassword != "" || cfg.Server.UIPasswordHash != ""}
	result.EnvironmentOverrides = []string{}
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(name, "PRISMCAT_") {
			result.EnvironmentOverrides = append(result.EnvironmentOverrides, name)
		}
	}
	deployment, readErr := os.ReadFile(filepath.Join(validated.Directory, "config", "deployment.json"))
	if readErr != nil {
		return result, readErr
	}
	var sourceDeployment struct {
		EnvironmentVariables []string `json:"environment_variables"`
	}
	if err = json.Unmarshal(deployment, &sourceDeployment); err != nil {
		return result, err
	}
	result.BackupEnvironmentVariables = sourceDeployment.EnvironmentVariables
	data, err = cfg.BackupSnapshot()
	if err != nil {
		return result, err
	}
	journal := restoreJournal{ID: uuid.NewString()}
	for _, name := range []string{"prismcat.db", "prismcat.db-wal", "prismcat.db-shm", "prismcat.db-journal", "blobs", "config.yaml"} {
		info, statErr := os.Lstat(filepath.Join(target, name))
		if statErr != nil && !os.IsNotExist(statErr) {
			return result, statErr
		}
		exists := statErr == nil
		if exists && (info.Mode()&os.ModeSymlink != 0 || (name != "blobs" && !info.Mode().IsRegular()) || (name == "blobs" && !info.IsDir())) {
			return result, errors.New("unsafe existing restore target")
		}
		if exists && !options.Replace && !options.DryRun {
			return result, errors.New("target contains data; inspect with --dry-run and use --replace to replace it")
		}
		journal.Entries = append(journal.Entries, restoreEntry{Name: name, HadOriginal: exists, Install: name == "prismcat.db" || name == "blobs" || name == "config.yaml"})
	}
	// Existing nonportable storage must not be silently left running while its
	// configuration is replaced. Lock all resources from the existing config too.
	if oldData, readErr := os.ReadFile(result.ConfigPath); readErr == nil {
		old, parseErr := config.Parse(oldData)
		if parseErr != nil {
			return result, parseErr
		}
		if !samePath(old.Storage.Database, cfg.Storage.Database) || !samePath(old.Storage.BlobDir, cfg.Storage.BlobDir) {
			return result, errors.New("existing config uses a different storage layout; restore into a new target directory")
		}
	}
	// Normalize interrupted jobs only in the extracted copy, never in the source.
	stagedDB := filepath.Join(validated.Directory, "database", "prismcat.db")
	if err = normalizeRestoredDatabase(ctx, stagedDB, validated.Manifest.CreatedAt); err != nil {
		return result, err
	}
	counts, refs, _, err := inspectDatabase(ctx, stagedDB)
	if err != nil {
		return result, err
	}
	if counts["request_logs"] != validated.Manifest.Counts["request_logs"] {
		return result, errors.New("database migration changed the number of request logs")
	}
	payloads := make(map[string]bool, len(validated.Manifest.Files))
	for _, file := range validated.Manifest.Files {
		payloads[file.Path] = true
	}
	for _, ref := range refs {
		name, pathErr := storage.BackupBlobPath(ref)
		if pathErr != nil || !payloads[name] {
			return result, errors.New("database migration references a missing body")
		}
	}
	if options.DryRun {
		return result, nil
	}
	var rollbackBytes int64
	for _, entry := range journal.Entries {
		if entry.HadOriginal {
			size, sizeErr := treeSize(filepath.Join(target, entry.Name))
			if sizeErr != nil {
				return result, sizeErr
			}
			rollbackBytes += size
		}
	}
	if err = requireSpace(target, rollbackBytes); err != nil {
		return result, err
	}
	stage := filepath.Join(target, ".prismcat-install-"+journal.ID)
	if err = os.Mkdir(stage, 0700); err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	if err = os.Rename(stagedDB, filepath.Join(stage, "prismcat.db")); err != nil {
		return result, err
	}
	blobSource := filepath.Join(validated.Directory, "blobs")
	if err = os.MkdirAll(blobSource, 0700); err != nil {
		return result, err
	}
	if err = os.Rename(blobSource, filepath.Join(stage, "blobs")); err != nil {
		return result, err
	}
	if err = writeSynced(filepath.Join(stage, "config.yaml"), data); err != nil {
		return result, err
	}
	if err = syncTree(stage); err != nil {
		return result, err
	}
	rollback := filepath.Join(target, ".prismcat-rollback-"+journal.ID)
	if err = os.Mkdir(rollback, 0700); err != nil {
		return result, err
	}
	if err = writeJSON(filepath.Join(rollback, "journal.json"), journal); err != nil {
		return result, err
	}
	if err = writeJSON(filepath.Join(target, journalName), journal); err != nil {
		return result, err
	}
	if err = install(ctx, target, stage, rollback, journal, nil); err != nil {
		rollbackErr := rollbackRestore(target, rollback, journal)
		return result, errors.Join(err, rollbackErr)
	}
	result.RollbackDirectory = rollback
	return result, nil
}

func install(ctx context.Context, target, stage, rollback string, journal restoreJournal, afterStep func(int) error) error {
	for i, entry := range journal.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.HadOriginal {
			if err := os.Rename(filepath.Join(target, entry.Name), filepath.Join(rollback, entry.Name)); err != nil {
				return err
			}
			if err := syncDirectory(rollback); err != nil {
				return err
			}
		}
		if entry.Install {
			if err := os.Rename(filepath.Join(stage, entry.Name), filepath.Join(target, entry.Name)); err != nil {
				return err
			}
		}
		if err := syncDirectory(target); err != nil {
			return err
		}
		if afterStep != nil {
			if err := afterStep(i); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(target, journalName)); err != nil {
		return err
	}
	return syncDirectory(target)
}

// Rollback recovers an interrupted restore, or explicitly reverts a completed
// one using the rollback directory printed by backup restore.
func Rollback(target, from string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	lock, err := instancelock.Acquire(filepath.Join(abs, "config.yaml"), filepath.Join(abs, "prismcat.db"), filepath.Join(abs, "blobs"))
	if err != nil {
		return err
	}
	defer lock.Close()
	journalPath := filepath.Join(abs, journalName)
	if from != "" {
		if PendingRestore(filepath.Join(abs, "config.yaml")) {
			return errors.New("unfinished restore: recover it without --from first")
		}
		resolved, err := filepath.Abs(from)
		if err != nil || filepath.Dir(resolved) != abs || !strings.HasPrefix(filepath.Base(resolved), ".prismcat-rollback-") {
			return errors.New("rollback directory must belong to target")
		}
		journalPath = filepath.Join(resolved, "journal.json")
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return err
	}
	var journal restoreJournal
	if err = json.Unmarshal(data, &journal); err != nil {
		return err
	}
	if _, err = uuid.Parse(journal.ID); err != nil {
		return errors.New("invalid restore journal")
	}
	rollback := filepath.Join(abs, ".prismcat-rollback-"+journal.ID)
	if from != "" && !samePath(from, rollback) {
		return errors.New("rollback journal mismatch")
	}
	if err = validateJournal(journal); err != nil {
		return err
	}
	if from != "" {
		for _, entry := range journal.Entries {
			if entry.HadOriginal {
				info, statErr := os.Lstat(filepath.Join(rollback, entry.Name))
				if statErr != nil || (entry.Name == "blobs" && !info.IsDir()) || (entry.Name != "blobs" && !info.Mode().IsRegular()) {
					return errors.New("rollback copy is incomplete")
				}
			}
		}
	}
	// An active journal also blocks normal startup if rollback is interrupted.
	if err = writeJSON(filepath.Join(abs, journalName), journal); err != nil {
		return err
	}
	return rollbackRestore(abs, rollback, journal)
}

func validateJournal(journal restoreJournal) error {
	wanted := []string{"prismcat.db", "prismcat.db-wal", "prismcat.db-shm", "prismcat.db-journal", "blobs", "config.yaml"}
	if len(journal.Entries) != len(wanted) {
		return errors.New("invalid restore journal entries")
	}
	for i, entry := range journal.Entries {
		if entry.Name != wanted[i] || entry.Install != (entry.Name == "prismcat.db" || entry.Name == "blobs" || entry.Name == "config.yaml") {
			return errors.New("invalid restore journal entry")
		}
	}
	return nil
}

func rollbackRestore(target, rollback string, journal restoreJournal) error {
	for i := len(journal.Entries) - 1; i >= 0; i-- {
		entry := journal.Entries[i]
		saved := filepath.Join(rollback, entry.Name)
		destination := filepath.Join(target, entry.Name)
		if _, err := os.Lstat(saved); err == nil {
			if err := os.RemoveAll(destination); err != nil {
				return err
			}
			// Copy rather than move: rollback remains restartable after power loss.
			if err := copyTree(saved, destination); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		} else if !entry.HadOriginal {
			if err := os.RemoveAll(destination); err != nil {
				return err
			}
		}
		if err := syncDirectory(target); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(target, journalName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(target)
}

func samePath(a, b string) bool {
	x, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	y, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	// Resolve aliases such as /var -> /private/var, but do not equate separate
	// hard-link names: replacing one name does not replace the other.
	if resolved, err := filepath.EvalSymlinks(x); err == nil {
		x = resolved
	}
	if resolved, err := filepath.EvalSymlinks(y); err == nil {
		y = resolved
	}
	return x == y
}

func writeSynced(filename string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

func normalizeRestoredDatabase(ctx context.Context, filename string, at time.Time) error {
	// This migration runs only against the verified staging copy. No background
	// retention or proxy workers are started.
	repo, err := storage.NewSQLiteRepository(filename)
	if err != nil {
		return err
	}
	if err = repo.Close(); err != nil {
		return err
	}
	db, err := storage.OpenBackupDatabase(filename, false)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE request_logs SET error='request interrupted by restore' WHERE status_code=0 AND COALESCE(error,'')=''"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE capture_runs SET stopped_at=?,revision=revision+1 WHERE stopped_at IS NULL", at.UnixMilli()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE experiment_runs SET document=json_set(document,'$.state','completed','$.finished_at',?,'$.updated_at',?,'$.revision',revision+1),revision=revision+1,updated_at=? WHERE json_extract(document,'$.state')='running'`, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano), at.UnixMilli())
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var busy, log, checkpointed int
	if err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil || busy != 0 {
		return fmt.Errorf("restore checkpoint failed: %v", err)
	}
	return nil
}
