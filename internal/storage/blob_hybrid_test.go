package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestHybridBodyThresholdAndReopenedStore(t *testing.T) {
	for _, size := range []int{0, 1, 128, 1000, chunkBodyThreshold - 1, chunkBodyThreshold, chunkBodyThreshold + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s := newChunkStore(t)
			body := bytes.Repeat([]byte("a"), size)
			ref, err := s.Put(context.Background(), body)
			if err != nil {
				t.Fatal(err)
			}
			_, h, _ := parseBlobRef(ref)
			path := s.pathFor(h)
			switch {
			case size >= chunkBodyThreshold:
				path = s.manifestPath(h)
			case size >= 128:
				path = s.compressedPath(h)
			}
			if _, err = os.Stat(path); err != nil {
				t.Fatal("wrong threshold representation", err)
			}
			reader, err := NewFileBlobStore(s.baseDir)
			if err != nil {
				t.Fatal(err)
			}
			assertBody(t, reader, ref, body)
			if ok, err := reader.Exists(context.Background(), ref); err != nil || !ok {
				t.Fatal("reopened store cannot find optimized body", err)
			}
			if same, err := reader.Put(context.Background(), body); err != nil || same != ref {
				t.Fatal("duplicate body changed reference", err)
			}
			if size >= 128 {
				if _, err = os.Stat(s.pathFor(h)); !os.IsNotExist(err) {
					t.Fatal("reopened store created a second representation", err)
				}
			}
			out, err := reader.OpenBackupBlob(ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(out)
			_ = out.Close()
			if err != nil || !bytes.Equal(got, body) {
				t.Fatal("portable body differs", err)
			}
			if size, err := reader.BackupBlobSize(ref); err != nil || size != int64(len(body)) {
				t.Fatal("backup reports compressed size", size, err)
			}
		})
	}
}

func TestLegacyShortCompressedBodyWithoutFrameSize(t *testing.T) {
	s := newChunkStore(t)
	body := bytes.Repeat([]byte("short SSE payload\n"), 10)
	encoder, err := zstd.NewWriter(nil, zstd.WithSingleSegment(false), zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	encoded := encoder.EncodeAll(body, nil)
	var header zstd.Header
	if err := header.Decode(encoded); err != nil || header.HasFCS {
		t.Fatalf("fixture must have no frame content size: %v", err)
	}
	ref := newSHA256Ref(sha256.Sum256(body))
	if err := writeBlobFile(s.compressedPath(mustHexHash(t, ref)), encoded); err != nil {
		t.Fatal(err)
	}
	assertBody(t, s, ref, body)
	if _, err := s.GetLimited(context.Background(), ref, int64(len(body)-1)); !errors.Is(err, ErrBlobTooLarge) {
		t.Fatalf("bounded read accepted an oversized body: %v", err)
	}
}

func TestHybridIncompressibleBodyStaysRaw(t *testing.T) {
	s := newChunkStore(t)
	body := make([]byte, 64<<10)
	r := rand.New(rand.NewPCG(37, 59))
	for i := range body {
		body[i] = byte(r.Uint32())
	}
	// Raw contents may start with zstd magic; suffix-based detection must not
	// mistake an arbitrary binary payload for a compressed representation.
	copy(body, []byte{0x28, 0xb5, 0x2f, 0xfd})
	ref, err := s.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	_, h, _ := parseBlobRef(ref)
	got, err := os.ReadFile(s.pathFor(h))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("incompressible body gained an encoding wrapper", err)
	}
	assertBody(t, s, ref, body)
}

func TestCompressedBodyCorruptionIsBoundedAndRejected(t *testing.T) {
	for _, damage := range []string{"truncated", "wrong-hash", "oversize-frame", "oversize-file", "symlink"} {
		t.Run(damage, func(t *testing.T) {
			s := newChunkStore(t)
			body := bytes.Repeat([]byte("agent result\n"), 5000)
			ref, err := s.Put(context.Background(), body)
			if err != nil {
				t.Fatal(err)
			}
			_, h, _ := parseBlobRef(ref)
			path := s.compressedPath(h)
			codecs, err := blobCodecs()
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "truncated":
				err = os.WriteFile(path, []byte{0x28, 0xb5}, 0600)
			case "wrong-hash":
				err = os.WriteFile(path, codecs.encoder.EncodeAll(bytes.Repeat([]byte("b"), len(body)), nil), 0600)
			case "oversize-frame":
				err = os.WriteFile(path, codecs.encoder.EncodeAll(bytes.Repeat([]byte("a"), chunkBodyThreshold), nil), 0600)
			case "oversize-file":
				err = os.WriteFile(path, make([]byte, chunkBodyThreshold), 0600)
			case "symlink":
				if err = os.Remove(path); err == nil {
					err = os.Symlink("missing-body", path)
					if err != nil {
						t.Skip("symlinks unavailable", err)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Get(context.Background(), ref); !errors.Is(err, ErrBlobCorrupt) {
				t.Fatal("accepted invalid compressed body", err)
			}
			if _, err = s.OpenBackupBlob(ref); !errors.Is(err, ErrBlobCorrupt) {
				t.Fatal("export accepted invalid compressed body", err)
			}
		})
	}
}

func TestConcurrentCompressedBodiesAndGC(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	body := bytes.Repeat([]byte("shared result\x00\xff\n"), 3000)
	ref, err := s.Put(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := s.Put(ctx, bytes.Repeat([]byte("stale small result"), 1000))
	if err != nil {
		t.Fatal(err)
	}
	ageStore(t, s)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 3 {
				if _, err := s.Put(ctx, body); err != nil {
					t.Error(err)
				}
				if b, err := s.Get(ctx, ref); err != nil || !bytes.Equal(b, body) {
					t.Error("compressed concurrent read", err)
				}
			}
		})
	}
	wg.Go(func() {
		for range 3 {
			if _, err := s.GarbageCollect(ctx, []string{ref}, time.Hour); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()
	if ok, err := s.Exists(ctx, orphan); err != nil || ok {
		t.Fatal("orphaned compressed body leaked", err)
	}
	assertBody(t, s, ref, body)
	if n, err := s.GarbageCollect(ctx, nil, 0); !errors.Is(err, ErrRefSetEmpty) || n != 0 {
		t.Fatal("empty reference guard missed compressed body", n, err)
	}
}

func TestCompressedBodyReuseProtectsUnpersistedLog(t *testing.T) {
	s := newChunkStore(t)
	ctx := context.Background()
	body := bytes.Repeat([]byte("agent history\n"), 3000)
	ref, err := s.Put(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	sentinel, err := s.Put(ctx, []byte("persisted unrelated log"))
	if err != nil {
		t.Fatal(err)
	}
	ageStore(t, s)
	if _, err = s.Put(ctx, body); err != nil {
		t.Fatal(err)
	}
	// The DB reference snapshot predates the new log reusing this old body.
	if _, err = s.GarbageCollect(ctx, []string{sentinel}, time.Hour); err != nil {
		t.Fatal(err)
	}
	assertBody(t, s, ref, body)
	ageStore(t, s)
	if _, err = s.GarbageCollect(ctx, []string{sentinel}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Exists(ctx, ref); err != nil || ok {
		t.Fatal("expired compressed body not collected", err)
	}
}
