package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerMutualExclusionCancellationAndRestartDiscovery(t *testing.T) {
	source, _, dir := backupFixture(t)
	started := make(chan struct{})
	source.Snapshot = func(ctx context.Context, _ string) error { close(started); <-ctx.Done(); return ctx.Err() }
	m := NewManager(source, filepath.Join(dir, "backups"))
	job, err := m.Create()
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err = m.Create(); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent backup accepted")
	}
	if err = m.Delete(job.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("running backup deleted")
	}
	if _, err = m.Open(job.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("partial backup downloadable")
	}
	m.Close()
	restarted := NewManager(source, filepath.Join(dir, "backups"))
	defer restarted.Close()
	jobs, err := restarted.List()
	if err != nil || len(jobs) != 1 || jobs[0].State != "failed" || jobs[0].Error != "backup_interrupted" {
		t.Fatalf("interrupted task not recovered: %+v %v", jobs, err)
	}
}

func TestManagerReadyArchivesSurviveRestart(t *testing.T) {
	source, repo, dir := backupFixture(t)
	saveFixtureLog(t, repo, "sample", "")
	m := NewManager(source, filepath.Join(dir, "backups"))
	job, err := m.Create()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := m.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State != "running" {
			if current.State != "completed" {
				t.Fatalf("failed: %+v", current)
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	m.Close()
	restarted := NewManager(source, filepath.Join(dir, "backups"))
	defer restarted.Close()
	file, err := restarted.Open(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err = restarted.Open("../../config.yaml"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unsafe id accepted")
	}
}
