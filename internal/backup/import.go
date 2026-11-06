package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/instancelock"
)

const MaxImportBytes int64 = 8 << 30
const importLifetime = 24 * time.Hour

var ErrImportLayout = errors.New("backup_import_layout")
var ErrImportState = errors.New("backup_import_state")

type ImportJob struct {
	ID                string           `json:"id"`
	State             string           `json:"state"`
	UploadedAt        time.Time        `json:"uploaded_at"`
	BackupCreatedAt   time.Time        `json:"backup_created_at"`
	SourceVersion     string           `json:"source_version"`
	TargetVersion     string           `json:"target_version"`
	UpgradeRequired   bool             `json:"upgrade_required"`
	Counts            map[string]int64 `json:"counts,omitempty"`
	Error             string           `json:"error,omitempty"`
	RollbackDirectory string           `json:"rollback_directory,omitempty"`
}

type importRecord struct {
	ImportJob
	SHA256 string `json:"sha256"`
}

func importDirectory(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "backups", "imports")
}
func importStatePath(dir string) string       { return filepath.Join(dir, "state.json") }
func importArchivePath(dir, id string) string { return filepath.Join(dir, id+".zip") }

func removeImportFiles(dir, id string) {
	_ = os.Remove(importArchivePath(dir, id))
	previews, _ := filepath.Glob(filepath.Join(dir, ".preview-"+id+"-*"))
	for _, path := range previews {
		_ = os.RemoveAll(path)
	}
}

func readImport(dir string) (*importRecord, error) {
	file, err := os.Open(importStatePath(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var record importRecord
	if err = json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&record); err != nil {
		return nil, ErrImportState
	}
	if id, err := uuid.Parse(record.ID); err != nil || id.String() != record.ID {
		return nil, ErrImportState
	}
	switch record.State {
	case "uploading", "checking", "ready", "pending", "applying", "completed", "failed", "cancelled":
	default:
		return nil, ErrImportState
	}
	return &record, nil
}

func importTarget(configPath string, storage config.StorageConfig) (string, error) {
	if filepath.Base(configPath) != "config.yaml" || storage.BlobStore != "fs" && storage.BlobStore != "" {
		return "", ErrImportLayout
	}
	target, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return "", err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if !samePath(storage.Database, filepath.Join(target, "prismcat.db")) || !samePath(storage.BlobDir, filepath.Join(target, "blobs")) {
		return "", ErrImportLayout
	}
	return target, nil
}

func (m *Manager) ImportSupported() bool {
	_, err := importTarget(m.source.Config.FilePath(), m.source.Config.StorageSnapshot())
	return err == nil
}

func (m *Manager) importStatusLocked() (*importRecord, error) {
	if m.importJob == nil {
		var err error
		m.importJob, err = readImport(importDirectory(m.source.Config.FilePath()))
		if err != nil {
			return nil, err
		}
	}
	return m.importJob, nil
}

func (m *Manager) ImportStatus() (*ImportJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.importStatusLocked()
	if err != nil || record == nil || record.State == "cancelled" {
		return nil, err
	}
	if record.State == "ready" && (time.Since(record.UploadedAt) > importLifetime || record.TargetVersion != config.Version) {
		record.State, record.Error = "failed", "backup_import_expired"
		dir := importDirectory(m.source.Config.FilePath())
		if err = writeJSON(importStatePath(dir), record); err != nil {
			return nil, err
		}
		removeImportFiles(dir, record.ID)
	}
	job := record.ImportJob
	return &job, nil
}

// UploadImport streams the ZIP to a private file. Only validation runs while the
// instance is live; installation is deferred until the next offline startup.
func (m *Manager) UploadImport(request context.Context, body io.ReadCloser, length int64) (ImportJob, error) {
	if !m.ImportSupported() {
		return ImportJob{}, ErrImportLayout
	}
	if length > MaxImportBytes {
		return ImportJob{}, errors.New("backup_import_size")
	}
	m.mu.Lock()
	previous, err := m.importStatusLocked()
	if err != nil {
		m.mu.Unlock()
		return ImportJob{}, err
	}
	if m.closed || m.active != nil || m.importing || previous != nil && (previous.State == "ready" || previous.State == "pending") {
		m.mu.Unlock()
		return ImportJob{}, ErrBusy
	}
	dir := importDirectory(m.source.Config.FilePath())
	if err = os.MkdirAll(dir, 0700); err != nil {
		m.mu.Unlock()
		return ImportJob{}, err
	}
	if previous != nil {
		removeImportFiles(dir, previous.ID)
	}
	record := &importRecord{ImportJob: ImportJob{ID: uuid.NewString(), State: "uploading", UploadedAt: time.Now().UTC()}}
	if err = writeJSON(importStatePath(dir), record); err != nil {
		m.mu.Unlock()
		return ImportJob{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.importJob, m.importing, m.importCancel = record, true, cancel
	m.wg.Add(1)
	m.mu.Unlock()
	stopRequest := context.AfterFunc(request, cancel)
	stopRead := context.AfterFunc(ctx, func() { _ = body.Close() })
	filename := importArchivePath(dir, record.ID)
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	hash := sha256.New()
	if err == nil {
		_, err = io.Copy(io.MultiWriter(file, hash), contextReader{ctx, io.LimitReader(body, MaxImportBytes+1)})
		if err == nil {
			err = file.Sync()
		}
		info, statErr := file.Stat()
		if err == nil && (statErr != nil || info.Size() > MaxImportBytes || info.Size() == 0) {
			err = errors.New("backup_import_size")
		}
		err = errors.Join(err, file.Close())
	}
	stopRequest()
	stopRead()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		m.finishImport(record.ID, "", importFailure(err))
		_ = os.Remove(filename)
		cancel()
		m.wg.Done()
		return ImportJob{}, err
	}
	m.mu.Lock()
	record.SHA256, record.State = hex.EncodeToString(hash.Sum(nil)), "checking"
	err = writeJSON(importStatePath(dir), record)
	job := record.ImportJob
	m.mu.Unlock()
	if err != nil {
		m.finishImport(record.ID, "", "backup_status_save_failed")
		_ = os.Remove(filename)
		cancel()
		m.wg.Done()
		return ImportJob{}, err
	}
	go func() {
		defer m.wg.Done()
		defer cancel()
		previewDir, err := os.MkdirTemp(dir, ".preview-"+record.ID+"-*")
		if err == nil {
			defer os.RemoveAll(previewDir)
			var preview RestoreResult
			preview, err = Restore(ctx, filename, RestoreOptions{Target: previewDir, DryRun: true, KeepServerConfig: m.source.Config.FilePath()})
			if err == nil {
				var target string
				target, err = importTarget(m.source.Config.FilePath(), m.source.Config.StorageSnapshot())
				if err == nil {
					var needed int64
					for _, file := range preview.Manifest.Files {
						needed += file.Size
						if file.Path == "database/prismcat.db" {
							needed += file.Size
						}
					}
					for _, name := range []string{"prismcat.db", "prismcat.db-wal", "prismcat.db-shm", "prismcat.db-journal", "blobs", "config.yaml"} {
						size, sizeErr := treeSize(filepath.Join(target, name))
						if sizeErr != nil && !os.IsNotExist(sizeErr) {
							err = sizeErr
							break
						}
						needed += size
					}
					if err == nil {
						err = requireSpace(target, needed)
					}
				}
			}
			if err == nil {
				m.mu.Lock()
				record.SourceVersion, record.TargetVersion = preview.Manifest.AppVersion, preview.TargetAppVersion
				record.BackupCreatedAt, record.UpgradeRequired = preview.Manifest.CreatedAt, preview.UpgradeRequired
				record.Counts = preview.Manifest.Counts
				m.mu.Unlock()
			}
		}
		code := ""
		if err != nil {
			code = importFailure(err)
			_ = os.Remove(filename)
		}
		m.finishImport(record.ID, "ready", code)
	}()
	return job, nil
}

func (m *Manager) finishImport(id, state, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.importJob == nil || m.importJob.ID != id {
		return
	}
	m.importing, m.importCancel = false, nil
	m.importJob.State, m.importJob.Error = state, code
	if code != "" {
		m.importJob.State = "failed"
	}
	if err := writeJSON(importStatePath(importDirectory(m.source.Config.FilePath())), m.importJob); err != nil {
		m.importJob.State, m.importJob.Error = "failed", "backup_status_save_failed"
	}
}

func (m *Manager) ConfirmImport(id string) (ImportJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.importStatusLocked()
	if err != nil {
		return ImportJob{}, err
	}
	if m.closed || m.importing || record == nil || record.ID != id || record.State != "ready" {
		return ImportJob{}, ErrImportState
	}
	if time.Since(record.UploadedAt) > importLifetime || record.TargetVersion != config.Version {
		return ImportJob{}, errors.New("backup_import_expired")
	}
	if _, err = importTarget(m.source.Config.FilePath(), m.source.Config.StorageSnapshot()); err != nil {
		return ImportJob{}, err
	}
	record.State = "pending"
	if err = writeJSON(importStatePath(importDirectory(m.source.Config.FilePath())), record); err != nil {
		// A directory sync can fail after replacement. Reflect the actual record
		// rather than reporting an unqueued operation that is already on disk.
		m.importJob, _ = readImport(importDirectory(m.source.Config.FilePath()))
		return ImportJob{}, err
	}
	return record.ImportJob, nil
}

func (m *Manager) CancelImport(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.importStatusLocked()
	if err != nil {
		return err
	}
	if m.importing {
		return ErrBusy
	}
	if m.closed || record == nil || record.ID != id {
		return ErrImportState
	}
	record.State = "cancelled"
	dir := importDirectory(m.source.Config.FilePath())
	if err = writeJSON(importStatePath(dir), record); err != nil {
		m.importJob, _ = readImport(dir)
		return err
	}
	if err = os.Remove(importArchivePath(dir, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func importFailure(err error) string {
	if errors.Is(err, ErrImportLayout) {
		return "backup_import_layout"
	}
	if errors.Is(err, context.Canceled) {
		return "backup_import_interrupted"
	}
	message := err.Error()
	if strings.Contains(message, "backup version incompatible") || strings.Contains(message, "backup format incompatible") {
		return "backup_import_incompatible"
	}
	if strings.Contains(message, "insufficient disk space") || strings.Contains(message, "no space left") {
		return "backup_space"
	}
	if strings.Contains(message, "backup_import_size") || strings.Contains(message, "request body too large") {
		return "backup_import_size"
	}
	return "backup_import_invalid"
}

// ApplyPendingImport is called before normal config locks, DB initialization or
// background workers. A persisted pending record is consumed at most once.
func ApplyPendingImport(ctx context.Context, configPath string) (*ImportJob, error) {
	dir := importDirectory(configPath)
	record, err := readImport(dir)
	if err != nil || record == nil {
		return nil, err
	}
	if record.State != "pending" && record.State != "applying" && record.State != "checking" && record.State != "uploading" {
		return nil, nil
	}
	lease, err := instancelock.Acquire(filepath.Join(dir, "startup"))
	if err != nil {
		return nil, err
	}
	defer lease.Close()
	record, err = readImport(dir)
	if err != nil || record == nil {
		return nil, err
	}
	if record.State != "pending" && record.State != "applying" && record.State != "checking" && record.State != "uploading" {
		return nil, nil
	}
	// Do not consume a pending operation while another instance still uses data.
	probe, err := instancelock.Acquire(configPath)
	if err != nil {
		return nil, err
	}
	probe.Close()
	filename := importArchivePath(dir, record.ID)
	if record.State != "pending" {
		if record.State == "applying" && PendingRestore(configPath) {
			if err = Rollback(filepath.Dir(configPath), ""); err != nil {
				return nil, err
			}
		}
		record.State, record.Error = "failed", "backup_import_interrupted"
	} else {
		cfg, loadErr := config.Load(configPath)
		if loadErr != nil {
			return nil, loadErr
		}
		target, layoutErr := importTarget(configPath, cfg.StorageSnapshot())
		if layoutErr != nil {
			record.State, record.Error = "failed", "backup_import_layout"
		} else {
			file, openErr := os.Open(filename)
			hash := sha256.New()
			if openErr == nil {
				_, openErr = io.Copy(hash, contextReader{ctx, file})
				openErr = errors.Join(openErr, file.Close())
			}
			if openErr != nil || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
				record.State, record.Error = "failed", "backup_import_invalid"
			} else {
				record.State = "applying"
				if err = writeJSON(importStatePath(dir), record); err != nil {
					return nil, err
				}
				result, restoreErr := Restore(ctx, filename, RestoreOptions{Target: target, Replace: true, KeepServerConfig: configPath})
				if restoreErr != nil {
					record.State, record.Error = "failed", importFailure(restoreErr)
				} else {
					record.State, record.Error, record.RollbackDirectory = "completed", "", result.RollbackDirectory
					record.TargetVersion, record.UpgradeRequired = result.TargetAppVersion, result.UpgradeRequired
				}
				if PendingRestore(configPath) {
					return nil, errors.New("unfinished restore: run backup rollback before starting")
				}
			}
		}
	}
	if err = writeJSON(importStatePath(dir), record); err != nil {
		return nil, err
	}
	removeImportFiles(dir, record.ID)
	return &record.ImportJob, nil
}
