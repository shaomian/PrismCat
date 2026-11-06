package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestChunkedAndLegacyBodiesExportAsPortableFormatOne(t *testing.T) {
	ctx := context.Background()
	source, repo, dir := backupFixture(t)
	var err error
	source.Blobs, err = storage.NewFileBlobStoreWithOptions(source.Config.StorageSnapshot().BlobDir, storage.FileBlobStoreOptions{Chunked: true})
	if err != nil {
		t.Fatal(err)
	}
	bodies := [][]byte{[]byte("small legacy body"), bytes.Repeat([]byte("shared agent history\n"), 50000)}
	bodies = append(bodies, append(bytes.Clone(bodies[1]), []byte("new command and result")...))
	var refs []string
	for i, body := range bodies {
		ref, err := source.Blobs.Put(ctx, body)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
		saveFixtureLog(t, repo, []string{"legacy", "first", "next"}[i], ref)
	}
	out := filepath.Join(dir, "portable.zip")
	manifest, err := Create(ctx, source, out, uuid.NewString(), "online", nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != 1 {
		t.Fatal("local optimization changed portable archive format")
	}
	z, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if strings.Contains(f.Name, ".manifest") || strings.Contains(f.Name, ".chunk") {
			t.Fatal("local chunk layout leaked into archive")
		}
	}
	_ = z.Close()
	target := filepath.Join(t.TempDir(), "restored")
	result, err := Restore(ctx, out, RestoreOptions{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(result.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, ref := range refs {
		got, err := s.Get(ctx, ref)
		if err != nil || !bytes.Equal(got, bodies[i]) {
			t.Fatal("restored body differs", err)
		}
		name, _ := storage.BackupBlobPath(ref)
		raw, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(raw, bodies[i]) {
			t.Fatal("restore requires the new local chunk decoder", err)
		}
	}
}

func TestBackupRejectsMissingSharedChunk(t *testing.T) {
	source, repo, dir := backupFixture(t)
	var err error
	source.Blobs, err = storage.NewFileBlobStoreWithOptions(source.Config.StorageSnapshot().BlobDir, storage.FileBlobStoreOptions{Chunked: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref, err := source.Blobs.Put(ctx, bytes.Repeat([]byte("shared context"), 50000))
	if err != nil {
		t.Fatal(err)
	}
	saveFixtureLog(t, repo, "missing-chunk", ref)
	removed := false
	_ = filepath.WalkDir(filepath.Join(dir, "blobs", "chunks"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !removed {
			removed = true
			return os.Remove(path)
		}
		return err
	})
	if !removed {
		t.Fatal("fixture was not chunked")
	}
	out := filepath.Join(dir, "broken.zip")
	if _, err = Create(ctx, source, out, uuid.NewString(), "online", nil); err == nil {
		t.Fatal("backup published a missing shared chunk")
	}
	if _, err = os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("partial archive was published")
	}
}
