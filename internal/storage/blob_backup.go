package storage

import (
	"context"
	"io"
)

// ProtectBackup must be acquired before taking the database snapshot, and held
// until every referenced blob has been copied. Put remains available.
func (s *FileBlobStore) ProtectBackup() func() {
	s.gcMu.RLock()
	return s.gcMu.RUnlock
}

func (s *FileBlobStore) OpenBackupBlob(ref string) (io.ReadCloser, error) {
	// Caller holds ProtectBackup for the whole snapshot/export. Stream original
	// bytes so format-1 archives remain portable, independent of local chunking.
	r, _, err := s.openBody(context.Background(), ref)
	return r, err
}

func (s *FileBlobStore) BackupBlobSize(ref string) (int64, error) {
	r, size, err := s.openBody(context.Background(), ref)
	if err != nil {
		return 0, err
	}
	return size, r.Close()
}

// BackupBlobPath canonicalizes legacy references as well as sha256: references.
func BackupBlobPath(ref string) (string, error) {
	_, hash, err := parseBlobRef(ref)
	if err != nil {
		return "", err
	}
	return "blobs/" + hash[:2] + "/" + hash, nil
}
