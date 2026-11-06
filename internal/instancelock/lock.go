// Package instancelock prevents a running server and offline maintenance from
// opening the same data. Lock files are deliberately retained after unlocking.
package instancelock

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Lock struct{ files []*os.File }

func Acquire(paths ...string) (*Lock, error) {
	unique := map[string]bool{}
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
			abs = resolved
		} else if !os.IsNotExist(resolveErr) {
			return nil, resolveErr
		}
		// Resolve existing ancestors so aliases cannot bypass the lock.
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return nil, err
		}
		unique[filepath.Join(parent, filepath.Base(abs))+".prismcat-lock"] = true
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	lock := &Lock{}
	for _, name := range names {
		file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
		if err == nil {
			err = lockFile(file)
		}
		if err != nil {
			if file != nil {
				_ = file.Close()
			}
			lock.Close()
			return nil, fmt.Errorf("data is in use or cannot be locked (%s): %w", name, err)
		}
		lock.files = append(lock.files, file)
	}
	return lock, nil
}

func (l *Lock) Close() {
	for i := len(l.files) - 1; i >= 0; i-- {
		_ = l.files[i].Close()
	}
	l.files = nil
}
