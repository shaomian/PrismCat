package backup

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func copyTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Mkdir(destination, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return syncDirectory(destination)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to copy non-regular file")
	}
	reader, err := os.Open(source)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, reader)
	if err == nil {
		err = writer.Sync()
	}
	closeErr := writer.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func treeSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular file in restore target")
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func syncTree(root string) error {
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return syncDirectory(name)
		}
		return nil
	})
}
