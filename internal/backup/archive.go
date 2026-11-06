// Package backup implements complete, versioned PrismCat migration archives.
package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

const FormatVersion = 1
const maxManifestBytes = 32 << 20
const maxFiles = 250000
const maxBytes int64 = 1 << 40 // Extraction ceiling, additionally bounded by free disk space.

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	FormatVersion int              `json:"format_version"`
	AppVersion    string           `json:"app_version"`
	ID            string           `json:"id"`
	CreatedAt     time.Time        `json:"created_at"`
	Mode          string           `json:"mode"`
	SchemaHash    string           `json:"schema_hash"`
	Counts        map[string]int64 `json:"counts"`
	Files         []File           `json:"files"`
}

type Progress struct {
	Phase string `json:"phase"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type Source struct {
	Snapshot func(context.Context, string) error
	Blobs    *storage.FileBlobStore
	Config   *config.Config
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func Create(ctx context.Context, source Source, destination, id, mode string, progress func(Progress)) (manifest Manifest, err error) {
	if source.Config == nil || source.Blobs == nil || source.Snapshot == nil {
		return manifest, errors.New("backup source unavailable")
	}
	if mode != "online" && mode != "offline" {
		return manifest, errors.New("invalid backup mode")
	}
	if parsed, parseErr := uuid.Parse(id); parseErr != nil || parsed.String() != id {
		return manifest, errors.New("invalid backup ID")
	}
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		return manifest, errors.New("backup destination already exists or is inaccessible")
	}
	dir := filepath.Dir(destination)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return manifest, err
	}
	// Backups must not participate in body storage accounting or garbage collection.
	storageCfg := source.Config.StorageSnapshot()
	archiveRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return manifest, err
	}
	blobRoot, err := filepath.EvalSymlinks(storageCfg.BlobDir)
	if err != nil {
		return manifest, err
	}
	archiveRoot, _ = filepath.Abs(archiveRoot)
	blobRoot, _ = filepath.Abs(blobRoot)
	if relative, relErr := filepath.Rel(blobRoot, archiveRoot); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return manifest, errors.New("backup directory must be outside body storage")
	}
	work, err := os.MkdirTemp(dir, ".prismcat-backup-"+id+"-*")
	if err != nil {
		return manifest, err
	}
	defer os.RemoveAll(work)
	state := Progress{Phase: "snapshot"}
	report := func() {
		if progress != nil {
			progress(state)
		}
	}
	report()
	unprotect := source.Blobs.ProtectBackup()
	defer unprotect()
	data, err := source.Config.BackupSnapshot()
	if err != nil {
		return manifest, err
	}
	if storageCfg.BlobStore != "" && storageCfg.BlobStore != "fs" {
		return manifest, errors.New("unsupported blob store")
	}
	if info, statErr := os.Stat(storageCfg.Database); statErr == nil {
		if err = requireSpace(dir, info.Size()*2); err != nil {
			return manifest, err
		}
	}
	database := filepath.Join(work, "snapshot.db")
	if err = source.Snapshot(ctx, database); err != nil {
		return manifest, fmt.Errorf("database snapshot: %w", err)
	}
	counts, refs, schema, err := inspectDatabase(ctx, database)
	if err != nil {
		return manifest, err
	}
	snapshotInfo, err := os.Stat(database)
	if err != nil {
		return manifest, err
	}
	estimate := snapshotInfo.Size() + int64(len(data)) + maxManifestBytes
	for _, ref := range refs {
		size, sizeErr := source.Blobs.BackupBlobSize(ref)
		if sizeErr != nil {
			return manifest, fmt.Errorf("missing body %s: %w", ref, sizeErr)
		}
		if size > maxBytes-estimate {
			return manifest, errors.New("backup size limit exceeded")
		}
		estimate += size
	}
	if err = requireSpace(dir, estimate+estimate/100); err != nil {
		return manifest, err
	}
	manifest = Manifest{FormatVersion: FormatVersion, AppVersion: config.Version, ID: id, CreatedAt: time.Now().UTC(), Mode: mode, SchemaHash: schema, Counts: counts}
	output := filepath.Join(work, "archive.zip")
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return manifest, err
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	add := func(name string, reader io.Reader) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		hash := sha256.New()
		size, err := io.Copy(io.MultiWriter(writer, hash), contextReader{ctx, reader})
		if err != nil {
			return err
		}
		if size > maxBytes-state.Bytes {
			return errors.New("backup size limit exceeded")
		}
		manifest.Files = append(manifest.Files, File{name, size, hex.EncodeToString(hash.Sum(nil))})
		state.Files++
		state.Bytes += size
		report()
		return nil
	}
	state.Phase = "archive"
	report()
	dbFile, err := os.Open(database)
	if err != nil {
		return manifest, err
	}
	err = add("database/prismcat.db", dbFile)
	_ = dbFile.Close()
	if err != nil {
		return manifest, err
	}
	if err = add("config/source.yaml", strings.NewReader(string(data))); err != nil {
		return manifest, err
	}
	envNames := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PRISMCAT_") {
			envNames = append(envNames, name)
		}
	}
	sort.Strings(envNames)
	deployment, _ := json.Marshal(map[string]any{"environment_variables": envNames, "database": storageCfg.Database, "blob_dir": storageCfg.BlobDir, "browser_data_included": false})
	if err = add("config/deployment.json", strings.NewReader(string(deployment))); err != nil {
		return manifest, err
	}
	for _, ref := range refs {
		name, err := storage.BackupBlobPath(ref)
		if err != nil {
			return manifest, err
		}
		blob, err := source.Blobs.OpenBackupBlob(ref)
		if err != nil {
			return manifest, fmt.Errorf("missing body %s: %w", ref, err)
		}
		err = add(name, blob)
		_ = blob.Close()
		if err != nil {
			return manifest, err
		}
		if manifest.Files[len(manifest.Files)-1].SHA256 != path.Base(name) {
			return manifest, fmt.Errorf("body hash mismatch: %s", ref)
		}
	}
	state.Phase = "verify"
	report()
	metadata, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil || len(metadata) > maxManifestBytes || len(manifest.Files) > maxFiles {
		return manifest, errors.New("backup file limit exceeded")
	}
	writer, err := archive.Create("manifest.json")
	if err != nil {
		return manifest, err
	}
	if _, err = writer.Write(metadata); err != nil {
		return manifest, err
	}
	if err = archive.Close(); err != nil {
		return manifest, err
	}
	if err = file.Sync(); err != nil {
		return manifest, err
	}
	if err = file.Close(); err != nil {
		return manifest, err
	}
	// A hard link publishes the completed archive without overwriting an existing file.
	if err = os.Link(output, destination); err != nil {
		return manifest, err
	}
	if err = syncDirectory(dir); err != nil {
		_ = os.Remove(destination)
		return manifest, err
	}
	return manifest, nil
}

type Validated struct {
	Manifest  Manifest
	Directory string
}

func (v *Validated) Close() { _ = os.RemoveAll(v.Directory) }

// Validate extracts only the small, fixed archive vocabulary into an isolated
// directory. No supplied paths are ever interpreted as installation paths.
func Validate(ctx context.Context, filename, temporaryParent string) (validated *Validated, err error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	if len(archive.File) > maxFiles+1 {
		return nil, errors.New("too many archive entries")
	}
	entries := map[string]*zip.File{}
	for _, file := range archive.File {
		if file.Name != "manifest.json" && !validPayloadPath(file.Name) {
			return nil, errors.New("invalid archive path")
		}
		if entries[file.Name] != nil || !file.Mode().IsRegular() {
			return nil, errors.New("duplicate or non-regular archive entry")
		}
		entries[file.Name] = file
	}
	metadata := entries["manifest.json"]
	if metadata == nil || metadata.UncompressedSize64 > maxManifestBytes {
		return nil, errors.New("missing or oversized manifest")
	}
	reader, err := metadata.Open()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	_ = reader.Close()
	if err != nil || len(data) > maxManifestBytes {
		return nil, errors.New("invalid manifest")
	}
	var manifest Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("backup format incompatible: archive format %d, supported format %d", manifest.FormatVersion, FormatVersion)
	}
	if err = checkAppVersion(manifest.AppVersion, config.Version); err != nil {
		return nil, err
	}
	if len(manifest.Files)+1 != len(entries) {
		return nil, errors.New("manifest file count mismatch")
	}
	var total int64
	listed := map[string]bool{}
	for _, item := range manifest.Files {
		entry := entries[item.Path]
		if !validPayloadPath(item.Path) || listed[item.Path] || entry == nil || item.Size < 0 || item.Size > maxBytes-total || uint64(item.Size) != entry.UncompressedSize64 {
			return nil, errors.New("invalid file manifest")
		}
		if len(item.SHA256) != 64 {
			return nil, errors.New("invalid file checksum")
		}
		listed[item.Path] = true
		total += item.Size
	}
	for _, required := range []string{"database/prismcat.db", "config/source.yaml", "config/deployment.json"} {
		if !listed[required] {
			return nil, errors.New("backup is missing a required file")
		}
	}
	dir, err := os.MkdirTemp(temporaryParent, ".prismcat-validate-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	if err = requireSpace(dir, total); err != nil {
		return nil, err
	}
	for _, item := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(item.Path, "config/") && item.Size > 8<<20 {
			return nil, errors.New("configuration too large")
		}
		target := filepath.Join(dir, filepath.FromSlash(item.Path))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		if err = extractFile(ctx, entries[item.Path], target, item); err != nil {
			return nil, err
		}
	}
	counts, refs, schema, err := inspectDatabase(ctx, filepath.Join(dir, "database", "prismcat.db"))
	if err != nil {
		return nil, err
	}
	if schema != manifest.SchemaHash || !equalCounts(counts, manifest.Counts) {
		return nil, errors.New("database manifest mismatch")
	}
	for _, ref := range refs {
		name, pathErr := storage.BackupBlobPath(ref)
		if pathErr != nil || !listed[name] {
			return nil, errors.New("database references a missing body")
		}
	}
	for _, item := range manifest.Files {
		if strings.HasPrefix(item.Path, "blobs/") && path.Base(item.Path) != item.SHA256 {
			return nil, errors.New("body content address mismatch")
		}
	}
	data, err = os.ReadFile(filepath.Join(dir, "config", "source.yaml"))
	if err != nil {
		return nil, err
	}
	if _, err = config.Parse(data); err != nil {
		return nil, fmt.Errorf("backup configuration: %w", err)
	}
	if err = syncTree(dir); err != nil {
		return nil, err
	}
	return &Validated{manifest, dir}, nil
}

func validPayloadPath(name string) bool {
	if name == "database/prismcat.db" || name == "config/source.yaml" || name == "config/deployment.json" {
		return true
	}
	if !strings.HasPrefix(name, "blobs/") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 3 || len(parts[1]) != 2 || len(parts[2]) != 64 || parts[1] != parts[2][:2] {
		return false
	}
	_, err := hex.DecodeString(parts[2])
	return err == nil && strings.ToLower(name) == name
}

func extractFile(ctx context.Context, entry *zip.File, target string, item File) error {
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), contextReader{ctx, io.LimitReader(reader, item.Size+1)})
	if err != nil {
		return err
	}
	if size != item.Size || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return fmt.Errorf("checksum mismatch: %s", item.Path)
	}
	return file.Sync()
}

func equalCounts(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || value != other {
			return false
		}
	}
	return true
}
