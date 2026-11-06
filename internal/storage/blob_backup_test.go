package storage

import (
	"context"
	"testing"
	"time"
)

func TestBackupProtectsSnapshotBodiesFromConcurrentGC(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Put(context.Background(), []byte("snapshot body"))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := store.Put(context.Background(), []byte("current body"))
	if err != nil {
		t.Fatal(err)
	}
	release := store.ProtectBackup()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := store.GarbageCollect(context.Background(), []string{retained}, 0)
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("GC bypassed protection: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err = store.Get(context.Background(), old); err != nil {
		t.Fatal("snapshot body removed")
	}
	// Writes remain available during an online backup.
	if _, err = store.Put(context.Background(), []byte("new body")); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("GC remained blocked")
	}
	if exists, err := store.Exists(context.Background(), old); err != nil || exists {
		t.Fatal("GC did not resume")
	}
}
