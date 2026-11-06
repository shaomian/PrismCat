package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/restic/chunker"
)

const (
	chunkBodyThreshold = 128 << 10
	chunkMinSize       = 16 << 10
	chunkMaxSize       = 256 << 10
	maxBodyChunks      = 131072
	maxBodyManifest    = 16 << 20
)

var ErrBlobCorrupt = errors.New("blob content or manifest is corrupt")

type bodyChunk struct {
	Hash string `json:"hash"`
	Size int    `json:"size"`
}

// Manifests refer directly to immutable chunks, never to another log/body.
// The public ref remains the SHA-256 of the complete original byte sequence.
type bodyManifest struct {
	Version int         `json:"version"`
	Size    int64       `json:"size"`
	Chunks  []bodyChunk `json:"chunks"`
}

type bodyCodecs struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

var blobCodecs = sync.OnceValues(func() (*bodyCodecs, error) {
	e, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(chunkMaxSize))
	if err != nil {
		return nil, err
	}
	d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(1<<20), zstd.WithDecoderMaxWindow(1<<20), zstd.WithDecodeAllCapLimit(true))
	return &bodyCodecs{e, d}, err
})

func (s *FileBlobStore) manifestPath(h string) string   { return s.pathFor(h) + ".manifest" }
func (s *FileBlobStore) compressedPath(h string) string { return s.pathFor(h) + ".zstd" }
func (s *FileBlobStore) chunkPath(h string) string {
	return filepath.Join(s.baseDir, "chunks", h[:2], h+".chunk")
}

func (s *FileBlobStore) putBody(ctx context.Context, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	h := hex.EncodeToString(sum[:])
	ref := newSHA256Ref(sum)
	// Preserve legacy files. Publishing a new representation never rewrites one.
	for _, path := range []string{s.pathFor(h), s.compressedPath(h), s.manifestPath(h)} {
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return "", ErrBlobCorrupt
			}
			now := time.Now()
			batch := newBlobWriteBatch(s.baseDir)
			batch.include(path)
			if path == s.manifestPath(h) {
				m, err := s.readManifest(h)
				if err != nil {
					return "", err
				}
				for _, part := range m.Chunks {
					batch.include(s.chunkPath(part.Hash))
					if err = os.Chtimes(s.chunkPath(part.Hash), now, now); err != nil {
						return "", err
					}
				}
			}
			if err = os.Chtimes(path, now, now); err != nil {
				return "", err
			}
			if s.chunkWrites || path != s.pathFor(h) {
				// A concurrent publisher may not have synced the root entry yet.
				if err = batch.syncDirs(); err != nil {
					return "", err
				}
			}
			return ref, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if !s.chunkWrites {
		return ref, writeBlobFile(s.pathFor(h), data, false)
	}
	codecs, err := blobCodecs()
	if err != nil {
		return "", err
	}
	batch := newBlobWriteBatch(s.baseDir)
	if len(data) < chunkBodyThreshold {
		// Keep small bodies in one file. A distinct suffix leaves all legacy
		// byte sequences unambiguous, even bodies beginning with zstd magic.
		path := s.pathFor(h)
		encoded := codecs.encoder.EncodeAll(data, nil)
		if len(encoded) < len(data) {
			path, data = s.compressedPath(h), encoded
		}
		if err = batch.write(path, data); err != nil {
			return "", err
		}
		return ref, batch.syncDirs()
	}
	m := bodyManifest{Version: 1, Size: int64(len(data))}
	c := chunker.NewBase(0x3DA3358B4DC173, chunker.WithBaseBoundaries(chunkMinSize, chunkMaxSize), chunker.WithBaseAverageBits(16))
	for len(data) > 0 {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n := c.NextSplitPoint(data)
		if n < 0 {
			n = len(data)
		}
		part := data[:n]
		sum := sha256.Sum256(part)
		ch := hex.EncodeToString(sum[:])
		path := s.chunkPath(ch)
		// Include reused directories too: another writer may have published
		// an immutable chunk but not yet synced its directory entry.
		batch.include(path)
		if info, statErr := os.Lstat(path); statErr == nil {
			if !info.Mode().IsRegular() {
				return "", ErrBlobCorrupt
			}
			now := time.Now()
			if err = os.Chtimes(path, now, now); err != nil {
				return "", err
			}
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		} else {
			compressed := codecs.encoder.EncodeAll(part, []byte{1})
			if len(compressed) >= len(part)+1 {
				compressed = append([]byte{0}, part...)
			}
			if err = batch.write(path, compressed); err != nil {
				return "", err
			}
		}
		m.Chunks = append(m.Chunks, bodyChunk{Hash: ch, Size: n})
		if len(m.Chunks) > maxBodyChunks {
			return "", errors.New("body has too many chunks")
		}
		data = data[n:]
	}
	metadata, err := json.Marshal(m)
	if err != nil || len(metadata) > maxBodyManifest {
		return "", ErrBlobCorrupt
	}
	// Persist all dependencies before publishing the root. Sync each touched
	// directory once per phase, rather than again after every shared chunk.
	if err = batch.syncDirs(); err != nil {
		return "", err
	}
	if err = batch.write(s.manifestPath(h), metadata); err != nil {
		return "", err
	}
	return ref, batch.syncDirs()
}

// writeBlobFile atomically publishes a file. Durable callers must also sync
// directory entries, using a body-scoped blobWriteBatch.
func writeBlobFile(path string, data []byte, durable bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil && durable {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		// Some platforms reject replacing an existing file. A concurrent writer
		// of the same immutable content-addressed object may have won the race.
		if info, statErr := os.Lstat(path); statErr != nil || !info.Mode().IsRegular() {
			return err
		}
	}
	return nil
}

func (s *FileBlobStore) readManifest(h string) (*bodyManifest, error) {
	info, err := os.Lstat(s.manifestPath(h))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBodyManifest {
		return nil, ErrBlobCorrupt
	}
	f, err := os.Open(s.manifestPath(h))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBodyManifest {
		return nil, ErrBlobCorrupt
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxBodyManifest+1))
	decoder.DisallowUnknownFields()
	var m bodyManifest
	if decoder.Decode(&m) != nil || decoder.Decode(new(any)) != io.EOF || m.Version != 1 || m.Size < chunkBodyThreshold || len(m.Chunks) == 0 || len(m.Chunks) > maxBodyChunks {
		return nil, ErrBlobCorrupt
	}
	var size int64
	for _, part := range m.Chunks {
		if !isBlobFileName(part.Hash) || part.Size <= 0 || part.Size > chunkMaxSize {
			return nil, ErrBlobCorrupt
		}
		size += int64(part.Size)
	}
	if size != m.Size {
		return nil, ErrBlobCorrupt
	}
	return &m, nil
}

func (s *FileBlobStore) openBody(ctx context.Context, ref string) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	_, h, err := parseBlobRef(ref)
	if err != nil {
		return nil, 0, err
	}
	if info, err := os.Lstat(s.pathFor(h)); err == nil {
		if !info.Mode().IsRegular() {
			return nil, 0, ErrBlobCorrupt
		}
		f, err := os.Open(s.pathFor(h))
		return f, info.Size(), err
	} else if !os.IsNotExist(err) {
		return nil, 0, err
	}
	if data, err := s.readCompressedBody(h); err == nil {
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
	} else if !os.IsNotExist(err) {
		return nil, 0, err
	}
	m, err := s.readManifest(h)
	if os.IsNotExist(err) {
		err = ErrBlobNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	return &chunkBodyReader{ctx: ctx, store: s, manifest: m, expected: h, hash: sha256.New()}, m.Size, nil
}

func (s *FileBlobStore) readCompressedBody(h string) ([]byte, error) {
	info, err := os.Lstat(s.compressedPath(h))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() >= chunkBodyThreshold {
		return nil, ErrBlobCorrupt
	}
	f, err := os.Open(s.compressedPath(h))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() >= chunkBodyThreshold {
		return nil, ErrBlobCorrupt
	}
	encoded, err := io.ReadAll(io.LimitReader(f, chunkBodyThreshold))
	if err != nil {
		return nil, err
	}
	var header zstd.Header
	if header.Decode(encoded) != nil || !header.HasFCS || header.FrameContentSize == 0 || header.FrameContentSize >= chunkBodyThreshold {
		return nil, ErrBlobCorrupt
	}
	codecs, err := blobCodecs()
	if err != nil {
		return nil, err
	}
	data, err := codecs.decoder.DecodeAll(encoded, make([]byte, 0, int(header.FrameContentSize)))
	if err != nil || len(data) != int(header.FrameContentSize) {
		return nil, ErrBlobCorrupt
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != h {
		return nil, ErrBlobCorrupt
	}
	return data, nil
}

func (s *FileBlobStore) readBody(ctx context.Context, ref string) ([]byte, error) {
	r, size, err := s.openBody(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// The logical size is known: avoid io.ReadAll's repeated growth and copies
	// for large agent histories. Chunk decompression stays bounded separately.
	if size < 0 || uint64(size) > uint64(^uint(0)>>1) {
		return nil, ErrBlobCorrupt
	}
	data := make([]byte, int(size))
	if _, err = io.ReadFull(r, data); err != nil {
		return nil, err
	}
	var end [1]byte
	if n, err := r.Read(end[:]); err != io.EOF || n != 0 {
		if err == nil {
			err = ErrBlobCorrupt
		}
		return nil, err
	}
	return data, nil
}

func (s *FileBlobStore) bodyExists(ctx context.Context, ref string) (bool, error) {
	r, _, err := s.openBody(ctx, ref)
	if errors.Is(err, ErrBlobNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer r.Close()
	if r, ok := r.(*chunkBodyReader); ok {
		for _, part := range r.manifest.Chunks {
			info, err := os.Lstat(s.chunkPath(part.Hash))
			if os.IsNotExist(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if !info.Mode().IsRegular() {
				return false, ErrBlobCorrupt
			}
		}
	}
	return true, nil
}

type chunkBodyReader struct {
	ctx      context.Context
	store    *FileBlobStore
	manifest *bodyManifest
	expected string
	hash     hash.Hash
	index    int
	current  *bytes.Reader
}

func (r *chunkBodyReader) Close() error { r.current = nil; return nil }
func (r *chunkBodyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	for r.current == nil || r.current.Len() == 0 {
		if r.index >= len(r.manifest.Chunks) {
			if hex.EncodeToString(r.hash.Sum(nil)) != r.expected {
				return 0, ErrBlobCorrupt
			}
			return 0, io.EOF
		}
		part := r.manifest.Chunks[r.index]
		data, err := r.store.readChunk(part)
		if err != nil {
			return 0, err
		}
		r.current = bytes.NewReader(data)
		r.index++
	}
	n, err := r.current.Read(p)
	_, _ = r.hash.Write(p[:n])
	return n, err
}

func (s *FileBlobStore) readChunk(part bodyChunk) ([]byte, error) {
	info, err := os.Lstat(s.chunkPath(part.Hash))
	if err != nil {
		return nil, fmt.Errorf("read body chunk: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > chunkMaxSize+1 {
		return nil, ErrBlobCorrupt
	}
	f, err := os.Open(s.chunkPath(part.Hash))
	if err != nil {
		return nil, fmt.Errorf("read body chunk: %w", err)
	}
	encoded, err := io.ReadAll(io.LimitReader(f, chunkMaxSize+2))
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, err
	}
	if len(encoded) < 2 || len(encoded) > chunkMaxSize+1 {
		return nil, ErrBlobCorrupt
	}
	var data []byte
	switch encoded[0] {
	case 0:
		data = encoded[1:]
	case 1:
		codecs, err := blobCodecs()
		if err != nil {
			return nil, err
		}
		data, err = codecs.decoder.DecodeAll(encoded[1:], make([]byte, 0, part.Size))
		if err != nil {
			return nil, fmt.Errorf("decode body chunk: %w", ErrBlobCorrupt)
		}
	default:
		return nil, ErrBlobCorrupt
	}
	sum := sha256.Sum256(data)
	if len(data) != part.Size || hex.EncodeToString(sum[:]) != part.Hash {
		return nil, ErrBlobCorrupt
	}
	return data, nil
}
