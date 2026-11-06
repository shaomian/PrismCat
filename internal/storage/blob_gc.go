package storage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func isManagedBlobName(name string) bool {
	return isBlobFileName(name) || isBlobFileName(strings.TrimSuffix(name, ".manifest")) || isBlobFileName(strings.TrimSuffix(name, ".chunk")) || isBlobFileName(strings.TrimSuffix(name, ".zstd"))
}

// Mark the complete live graph before deleting anything. Fresh unpublished
// manifests are roots too: the async log may not be in the DB snapshot yet.
func (s *FileBlobStore) collectBodies(ctx context.Context, refs []string, minAge time.Duration) (int, error) {
	roots := make(map[string]bool, len(refs))
	for _, ref := range refs {
		_, h, err := parseBlobRef(ref)
		if err != nil {
			return 0, err
		}
		roots[h] = true
	}
	if len(roots) == 0 {
		empty, err := s.isEmpty()
		if err != nil {
			return 0, err
		}
		if !empty {
			return 0, ErrRefSetEmpty
		}
	}
	liveChunks := map[string]bool{}
	mark := func(h string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		m, err := s.readManifest(h)
		if err != nil {
			return err
		}
		for _, part := range m.Chunks {
			liveChunks[part.Hash] = true
		}
		return nil
	}
	for h := range roots {
		whole := false
		for _, path := range []string{s.pathFor(h), s.compressedPath(h)} {
			if info, err := os.Lstat(path); err == nil {
				if !info.Mode().IsRegular() {
					return 0, ErrBlobCorrupt
				}
				whole = true
				break
			} else if !os.IsNotExist(err) {
				return 0, err
			}
		}
		if whole {
			continue
		}
		if err := mark(h); err != nil {
			return 0, err
		}
	}
	var cutoff time.Time
	if minAge > 0 {
		cutoff = time.Now().Add(-minAge)
	}
	// Fresh roots protect shared dependencies even when absent from the DB
	// snapshot. Put also refreshes reused chunks for publications after this walk.
	err := filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		h := strings.TrimSuffix(d.Name(), ".manifest")
		if d.IsDir() || h == d.Name() || !isBlobFileName(h) || roots[h] || cutoff.IsZero() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.ModTime().Before(cutoff) {
			return mark(h)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	deleted := 0
	err = filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !isManagedBlobName(name) && (!strings.HasPrefix(name, ".tmp-") || cutoff.IsZero()) {
			return nil
		}
		if strings.HasSuffix(name, ".chunk") {
			if liveChunks[strings.TrimSuffix(name, ".chunk")] {
				return nil
			}
		} else if roots[strings.TrimSuffix(strings.TrimSuffix(name, ".manifest"), ".zstd")] {
			return nil
		}
		s.ioMu.Lock()
		defer s.ioMu.Unlock()
		// Re-stat under the deletion lock: Put may have reused this file since
		// WalkDir observed it. Do not lock the store during the full mark/walk.
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !cutoff.IsZero() && !info.ModTime().Before(cutoff) {
			return nil
		}
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		deleted++
		return nil
	})
	// Empty directories are harmless; remove them bottom-up on a best-effort basis.
	if err == nil {
		var dirs []string
		_ = filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && path != s.baseDir {
				dirs = append(dirs, path)
			}
			return nil
		})
		for i := len(dirs) - 1; i >= 0; i-- {
			s.ioMu.Lock()
			_ = os.Remove(dirs[i])
			s.ioMu.Unlock()
		}
	}
	return deleted, err
}
