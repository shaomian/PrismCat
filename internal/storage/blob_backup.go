package storage

import (
	"fmt"
	"io"
	"os"
)

// ProtectBackup must be acquired before taking the database snapshot, and held
// until every referenced blob has been copied. Put remains available.
func (s *FileBlobStore) ProtectBackup() func() {
	s.gcMu.RLock()
	return s.gcMu.RUnlock
}

func (s *FileBlobStore) OpenBackupBlob(ref string) (io.ReadCloser, error) {
	_, hash, err := parseBlobRef(ref)
	if err != nil {
		return nil, err
	}
	path := s.pathFor(hash)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("blob must be a regular file")
	}
	return os.Open(path)
}

func (s *FileBlobStore) BackupBlobSize(ref string) (int64, error) {
	_, hash, err := parseBlobRef(ref)
	if err != nil {
		return 0, err
	}
	info, err := os.Lstat(s.pathFor(hash))
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("blob must be a regular file")
	}
	return info.Size(), nil
}

// BackupBlobPath canonicalizes legacy references as well as sha256: references.
func BackupBlobPath(ref string) (string, error) {
	_, hash, err := parseBlobRef(ref)
	if err != nil {
		return "", err
	}
	return "blobs/" + hash[:2] + "/" + hash, nil
}
