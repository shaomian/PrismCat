//go:build !windows

package backup

import (
	"golang.org/x/sys/unix"
	"os"
)

func availableSpace(dir string) (uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(dir, &stat)
	return uint64(stat.Bavail) * uint64(stat.Bsize), err
}
func replaceFile(source, target string) error { return os.Rename(source, target) }
func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
