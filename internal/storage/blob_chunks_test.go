package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func contextBody(size int) []byte {
	r := rand.New(rand.NewPCG(13, 71))
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(' ' + r.IntN(90))
	}
	return b
}

func newChunkStore(t *testing.T) *FileBlobStore {
	t.Helper()
	s, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func assertBody(t *testing.T, s *FileBlobStore, ref string, expected []byte) {
	t.Helper()
	got, err := s.Get(context.Background(), ref)
	if err != nil || !bytes.Equal(got, expected) {
		t.Fatalf("body round trip failed: %v", err)
	}
}

func ageStore(t *testing.T, s *FileBlobStore) {
	t.Helper()
	old := time.Now().Add(-2 * time.Hour)
	err := filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			return os.Chtimes(path, old, old)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChunkedBodiesShareBytesWithoutDependingOnPreviousLogs(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	base := contextBody(2 << 20)
	first, err := s.Put(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	// Insert into the middle as well as appending: fixed-position chunking
	// would lose most reuse after the insertion.
	next := append(bytes.Clone(base[:70000]), []byte("new command\n")...)
	next = append(next, base[70000:]...)
	next = append(next, []byte("\x00\xff final result")...)
	second, err := s.Put(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	assertBody(t, s, first, base)
	assertBody(t, s, second, next)
	sum := sha256.Sum256(next)
	if second != newSHA256Ref(sum) {
		t.Fatal("logical ref no longer hashes original bytes")
	}
	_, h, _ := parseBlobRef(second)
	m, err := s.readManifest(h)
	if err != nil || len(m.Chunks) < 2 {
		t.Fatalf("not chunked: %v", err)
	}
	var physical int64
	_ = filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, _ := d.Info()
			physical += info.Size()
		}
		return err
	})
	if physical >= int64(len(base)+len(next))*7/10 {
		t.Fatalf("inserted context failed to share data: %d", physical)
	}
	ageStore(t, s)
	if _, err = s.GarbageCollect(ctx, []string{second}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.Exists(ctx, first); err != nil || exists {
		t.Fatalf("old manifest still present: %v", err)
	}
	assertBody(t, s, second, next)
	// Backups use a bounded streaming reader, and must export exactly the raw
	// bytes, including non-UTF8 data, without a storage-format dependency.
	release := s.ProtectBackup()
	r, err := s.OpenBackupBlob(second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	release()
	if err != nil || !bytes.Equal(got, next) {
		t.Fatalf("portable backup stream changed: %v", err)
	}
	if size, err := s.BackupBlobSize(second); err != nil || size != int64(len(next)) {
		t.Fatalf("backup used physical size: %d %v", size, err)
	}
}

func TestLegacyBodiesRemainReadableAndAreNotRewritten(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	body := contextBody(512 << 10)
	sum := sha256.Sum256(body)
	h := hex.EncodeToString(sum[:])
	ref := newSHA256Ref(sum)
	if err := writeBlobFile(s.pathFor(h), body); err != nil {
		t.Fatal(err)
	}
	assertBody(t, s, ref, body)
	if got, err := s.Put(ctx, body); err != nil || got != ref {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.manifestPath(h)); !os.IsNotExist(err) {
		t.Fatal("legacy data rewritten")
	}
	small := []byte("ordinary small body")
	ref, err := s.Put(ctx, small)
	if err != nil {
		t.Fatal(err)
	}
	_, h, _ = parseBlobRef(ref)
	b, err := os.ReadFile(s.pathFor(h))
	if err != nil || !bytes.Equal(b, small) {
		t.Fatal("small body changed representation")
	}
}

func TestReopenedStoreContinuesHybridWrites(t *testing.T) {
	s := newChunkStore(t)
	body := contextBody(256 << 10)
	ref, err := s.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileBlobStore(s.baseDir)
	if err != nil {
		t.Fatal(err)
	}
	assertBody(t, reopened, ref, body)
	newBody := append(bytes.Clone(body), []byte("next")...)
	newRef, err := reopened.Put(context.Background(), newBody)
	if err != nil {
		t.Fatal(err)
	}
	_, h, _ := parseBlobRef(newRef)
	if _, err = os.Stat(reopened.manifestPath(h)); err != nil {
		t.Fatal("reopened store did not keep chunking new bodies", err)
	}
	assertBody(t, s, newRef, newBody)
}

func TestFreshManifestProtectsOldSharedChunksBeforeLogPersistence(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	body := contextBody(512 << 10)
	old, err := s.Put(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	sentinel, err := s.Put(ctx, []byte("persisted unrelated log"))
	if err != nil {
		t.Fatal(err)
	}
	ageStore(t, s)
	freshBody := append(bytes.Clone(body), []byte("new result")...)
	fresh, err := s.Put(ctx, freshBody)
	if err != nil {
		t.Fatal(err)
	}
	// Only the unrelated log is in the DB snapshot. Both old and new bodies
	// share old chunks, but the recent manifest must protect the new graph.
	if _, err = s.GarbageCollect(ctx, []string{sentinel}, time.Hour); err != nil {
		t.Fatal(err)
	}
	assertBody(t, s, fresh, freshBody)
	if ok, err := s.Exists(ctx, old); err != nil || ok {
		t.Fatal("stale body was not reclaimed", err)
	}
	ageStore(t, s)
	// Reusing an existing body refreshes the root, not every shared chunk.
	if _, err = s.Put(ctx, freshBody); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GarbageCollect(ctx, []string{sentinel}, time.Hour); err != nil {
		t.Fatal(err)
	}
	assertBody(t, s, fresh, freshBody)
	ageStore(t, s)
	if _, err = s.GarbageCollect(ctx, []string{sentinel}, time.Hour); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.baseDir, "chunks"))
	if len(entries) != 0 {
		t.Fatal("orphaned shared chunks leaked")
	}
}

func TestChunkCorruptionAndMalformedManifestsFailClosed(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt", "oversize", "manifest", "version", "traversal", "total", "reorder"} {
		t.Run(damage, func(t *testing.T) {
			s := newChunkStore(t)
			ctx := context.Background()
			ref, err := s.Put(ctx, contextBody(512<<10))
			if err != nil {
				t.Fatal(err)
			}
			_, h, _ := parseBlobRef(ref)
			m, err := s.readManifest(h)
			if err != nil {
				t.Fatal(err)
			}
			path := s.chunkPath(m.Chunks[0].Hash)
			switch damage {
			case "missing":
				err = os.Remove(path)
			case "corrupt":
				err = os.WriteFile(path, []byte{1, 2, 3}, 0600)
			case "oversize":
				err = os.WriteFile(path, make([]byte, chunkMaxSize+2), 0600)
			case "manifest":
				err = os.WriteFile(s.manifestPath(h), []byte(`{"version":1}`), 0600)
			case "version":
				m.Version = 2
			case "traversal":
				m.Chunks[0].Hash = "../../outside"
			case "total":
				m.Size++
			case "reorder":
				m.Chunks[0], m.Chunks[1] = m.Chunks[1], m.Chunks[0]
			}
			if err != nil {
				t.Fatal(err)
			}
			if damage == "version" || damage == "traversal" || damage == "total" || damage == "reorder" {
				b, _ := json.Marshal(m)
				if err = os.WriteFile(s.manifestPath(h), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.Get(ctx, ref); err == nil {
				t.Fatal("damaged data was silently accepted")
			}
			out, err := s.OpenBackupBlob(ref)
			if err == nil {
				_, err = io.Copy(io.Discard, out)
				_ = out.Close()
			}
			if err == nil {
				t.Fatal("damaged body exported")
			}
		})
	}
}

func TestInvalidLiveManifestStopsGCBeforeDeletingAnything(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	ref, err := s.Put(ctx, contextBody(512<<10))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := s.Put(ctx, []byte("must survive failed mark phase"))
	if err != nil {
		t.Fatal(err)
	}
	ageStore(t, s)
	_, h, _ := parseBlobRef(ref)
	if err = os.WriteFile(s.manifestPath(h), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := s.GarbageCollect(ctx, []string{ref}, 0); err == nil || n != 0 {
		t.Fatalf("swept with broken root: %d %v", n, err)
	}
	assertBody(t, s, orphan, []byte("must survive failed mark phase"))
}

func TestConcurrentChunkPublicationAndGC(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	sentinel, err := s.Put(ctx, []byte("persisted"))
	if err != nil {
		t.Fatal(err)
	}
	body := contextBody(256 << 10)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for k := 0; k < 3; k++ {
				ref, err := s.Put(ctx, body)
				if err != nil {
					t.Error(err)
					return
				}
				if b, err := s.Get(ctx, ref); err != nil || !bytes.Equal(b, body) {
					t.Error("concurrent round trip", err)
				}
			}
		})
	}
	wg.Go(func() {
		for i := 0; i < 5; i++ {
			if _, err := s.GarbageCollect(ctx, []string{sentinel}, time.Hour); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()
}

func TestChunkedStoreCancellationAndEmptyReferenceGuard(t *testing.T) {
	s := newChunkStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Put(ctx, contextBody(128<<10)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ref, err := s.Put(context.Background(), contextBody(128<<10))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, ref); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n, err := s.GarbageCollect(context.Background(), nil, 0); !errors.Is(err, ErrRefSetEmpty) || n != 0 {
		t.Fatal(n, err)
	}
}
