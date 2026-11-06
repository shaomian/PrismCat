package storage

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// A batch is scoped to one body, never shared by concurrent writers. Files are
// synced individually before rename; directory entries are synced together.
type blobWriteBatch struct {
	root string
	dirs map[string]struct{}
}

func newBlobWriteBatch(root string) *blobWriteBatch {
	return &blobWriteBatch{root: filepath.Clean(root), dirs: make(map[string]struct{})}
}

func (b *blobWriteBatch) include(path string) {
	dir := filepath.Dir(path)
	b.dirs[dir] = struct{}{}
	b.dirs[filepath.Dir(dir)] = struct{}{}
	b.dirs[b.root] = struct{}{} // persists a newly created chunks directory too
}

func (b *blobWriteBatch) write(path string, data []byte) error {
	if err := writeBlobFile(path, data, true); err != nil {
		return err
	}
	b.include(path)
	return nil
}

func (b *blobWriteBatch) syncDirs() error {
	if runtime.GOOS != "windows" {
		dirs := make([]string, 0, len(b.dirs))
		for dir := range b.dirs {
			dirs = append(dirs, dir)
		}
		// Persist children before parents, including newly created prefixes.
		sort.Slice(dirs, func(i, j int) bool {
			if len(dirs[i]) != len(dirs[j]) {
				return len(dirs[i]) > len(dirs[j])
			}
			return dirs[i] < dirs[j]
		})
		for _, dir := range dirs {
			d, err := os.Open(dir)
			if err != nil {
				return err
			}
			if err = errors.Join(d.Sync(), d.Close()); err != nil {
				return err
			}
		}
	}
	clear(b.dirs)
	return nil
}
