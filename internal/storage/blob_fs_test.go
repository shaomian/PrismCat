package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 引用列表为空但仓库非空,曾经会把整个 blob 仓库删空 —— 只要进程被指向另一个
// 数据库(还原备份、换库测试、配置写错),ListBlobRefs 就返回空列表,而 GC 照
// 字面执行。blob 目录的路径不是从数据库路径推导的,两者只是约定配对。
func TestGarbageCollectRefusesEmptyRefSet(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ref, err := store.Put(ctx, []byte("payload that belongs to another database"))
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(ctx, nil, 0)
	if !errors.Is(err, ErrRefSetEmpty) {
		t.Fatalf("want ErrRefSetEmpty, got %v", err)
	}
	if deleted != 0 {
		t.Fatalf("want 0 deleted, got %d", deleted)
	}

	// 数据必须还在
	if _, err := store.Get(ctx, ref); err != nil {
		t.Fatalf("blob was deleted despite the guard: %v", err)
	}
}

// 仓库本来就是空的时候不该报错 —— 那是全新安装的正常状态。
func TestGarbageCollectEmptyRefSetOnEmptyStore(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(context.Background(), nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("want 0 deleted, got %d", deleted)
	}
}

// 守卫不能挡住正常回收:引用列表非空时,未被引用的 blob 照删。
func TestGarbageCollectStillCollectsUnreferenced(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	keep, err := store.Put(ctx, []byte("still referenced"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := store.Put(ctx, []byte("no longer referenced"))
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(ctx, []string{keep}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("want 1 deleted, got %d", deleted)
	}
	if _, err := store.Get(ctx, keep); err != nil {
		t.Fatalf("referenced blob was deleted: %v", err)
	}
	if _, err := store.Get(ctx, orphan); err == nil {
		t.Fatal("orphan blob should have been deleted")
	}
}

// 去重命中必须刷新 mtime：否则「新日志引用了旧 blob」在 GC 的 minAge
// 防线看来仍是超龄孤儿，引用快照落后于日志写入时会被误删。
func TestPutDedupRefreshesMTime(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ref, err := store.Put(ctx, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	_, hexHash, _ := parseBlobRef(ref)
	path := store.pathFor(hexHash)

	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Put(ctx, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("dedup hit did not refresh mtime: %v", info.ModTime())
	}
}

// 崩溃残留的 .tmp- 写入中间文件应当被 GC 清掉（超龄时），
// 但进行中的写入（新鲜 .tmp-）不能动。
func TestGarbageCollectRemovesStaleTmpFiles(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ref, err := store.Put(ctx, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(store.pathFor(mustHexHash(t, ref)))
	staleTmp := filepath.Join(dir, ".tmp-deadbeef-1")
	if err := os.WriteFile(staleTmp, []byte("crash leftover"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(staleTmp, stale, stale); err != nil {
		t.Fatal(err)
	}
	freshTmp := filepath.Join(dir, ".tmp-inflight-2")
	if err := os.WriteFile(freshTmp, []byte("being written"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(ctx, []string{ref}, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("want 1 deleted (stale tmp only), got %d", deleted)
	}
	if _, err := os.Stat(staleTmp); !os.IsNotExist(err) {
		t.Fatal("stale .tmp- file should have been removed")
	}
	if _, err := os.Stat(freshTmp); err != nil {
		t.Fatalf("in-flight .tmp- file must survive: %v", err)
	}
	if _, err := store.Get(ctx, ref); err != nil {
		t.Fatalf("referenced blob should survive: %v", err)
	}
}

func mustHexHash(t *testing.T, ref string) string {
	t.Helper()
	_, hexHash, err := parseBlobRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	return hexHash
}
