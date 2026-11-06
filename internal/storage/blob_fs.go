package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrRefSetEmpty 表示引用列表为空,但仓库里确实有 blob。
//
// blob 目录的路径不是从数据库路径推导的,两者只是约定配对。一旦进程被指向
// 另一个数据库(还原备份、换库测试、配置写错),引用列表就会是空的,而照字面
// 执行 GC 会把整个仓库删空 —— 那些 blob 属于原来那个库,不是孤儿。
//
// 代价完全不对等:误删是静默且永久的数据丢失,误留只是占点磁盘。所以这里宁可
// 不删。真的想清空,先把日志删掉让仓库自然变空,或者手动删目录。
var ErrRefSetEmpty = errors.New("blob gc: reference set is empty but the store is not; refusing to delete")

// FileBlobStore stores blobs on the local filesystem under a content-addressed path.
// Legacy layout: <baseDir>/<hash[:2]>/<hash>. Optimized bodies use a sibling
// .zstd or .manifest with shared chunks; logical references are unchanged.
type FileBlobStore struct {
	baseDir string
	gcMu    sync.RWMutex
	ioMu    sync.RWMutex // protects chunk publication/read against GC; separate from backup protection
}

func NewFileBlobStore(baseDir string) (*FileBlobStore, error) {
	if baseDir == "" {
		return nil, errors.New("blob base dir is empty")
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &FileBlobStore{baseDir: baseDir}, nil
}

func (s *FileBlobStore) Put(ctx context.Context, data []byte) (string, error) {
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	return s.putBody(ctx, data)
}

func (s *FileBlobStore) Get(ctx context.Context, ref string) ([]byte, error) {
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	return s.readBody(ctx, ref)
}

func (s *FileBlobStore) Exists(ctx context.Context, ref string) (bool, error) {
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	return s.bodyExists(ctx, ref)
}

// isEmpty 报告仓库里是否一个 blob 文件都没有。命中第一个就返回,不遍历全部。
func (s *FileBlobStore) isEmpty() (bool, error) {
	found := false
	errFound := errors.New("found")
	err := filepath.WalkDir(s.baseDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isManagedBlobName(d.Name()) {
			return nil
		}
		found = true
		return errFound
	})
	if errors.Is(err, errFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return !found, nil
}

// isBlobFileName 判断一个文件名是否是内容寻址的 blob(而非 .tmp- 临时文件)。
func isBlobFileName(name string) bool {
	if strings.HasPrefix(name, ".tmp-") || len(name) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

// GarbageCollect marks the live body graph then removes unreferenced files.
// Live instances must use a positive minAge to protect not-yet-persisted logs;
// zero is suitable only for a coherent offline reference set.
func (s *FileBlobStore) GarbageCollect(ctx context.Context, referencedRefs []string, minAge time.Duration) (int, error) {
	s.gcMu.Lock()
	defer s.gcMu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	return s.collectBodies(ctx, referencedRefs, minAge)
}

func (s *FileBlobStore) pathFor(hexHash string) string {
	prefix := hexHash[:2]
	return filepath.Join(s.baseDir, prefix, hexHash)
}
