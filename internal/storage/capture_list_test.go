package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestCapturePagesKeepActiveRunsVisibleAndReachAllHistory(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	active, err := r.StartCaptureRun("old active capture", "active", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 124 {
		run, err := r.StartCaptureRun(fmt.Sprintf("history %d", i), "history", false)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.StopCaptureRun(run.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Make the active run older than every stopped run, independent of clock resolution.
	if _, err := r.db.Exec("UPDATE capture_runs SET started_at=1 WHERE id=?", active.ID); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for offset := 0; offset < 125; offset += 20 {
		page, total, err := r.ListCaptureRunsPage(20, offset)
		if err != nil || total != 125 || len(page) != min(20, 125-offset) {
			t.Fatalf("page at %d: len=%d total=%d err=%v", offset, len(page), total, err)
		}
		if offset == 0 && page[0].ID != active.ID {
			t.Fatal("old active capture was hidden behind history")
		}
		for _, run := range page {
			if seen[run.ID] {
				t.Fatalf("duplicate capture across pages: %s", run.ID)
			}
			seen[run.ID] = true
			if run.RequestCount != 0 || run.UsageInput != nil || run.UsageOutput != nil {
				t.Fatalf("empty capture acquired invented usage: %+v", run)
			}
		}
	}
	page, total, err := r.ListCaptureRunsPage(20, 140)
	if err != nil || len(page) != 0 || page == nil || total != 125 {
		t.Fatalf("out of range: %v %d %v", page, total, err)
	}
	legacy, err := r.ListCaptureRuns()
	if err != nil || len(legacy) != 100 {
		t.Fatalf("legacy list: len=%d err=%v", len(legacy), err)
	}
}

func TestCapturePageStatisticsAndActiveRegistrySnapshot(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	run, err := r.StartCaptureRun("usage", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	input, output := int64(120), int64(30)
	for i := range 2 {
		entry := newTestLog(fmt.Sprintf("capture-log-%d", i), time.UnixMilli(1000+int64(i)*1000))
		entry.CaptureRunID = run.ID
		entry.Latency = int64(200 + i*100)
		if i == 0 {
			entry.UsageInputTokens, entry.UsageOutputTokens = &input, &output
		}
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	page, _, err := r.ListCaptureRunsPage(20, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := page[0]
	if got.RequestCount != 2 || got.TotalLatency != 500 || got.WallTime != 1300 || got.UsageMissing != 1 || got.UsageInput == nil || *got.UsageInput != 120 || got.UsageOutput == nil || *got.UsageOutput != 30 {
		t.Fatalf("incorrect aggregate: %+v", got)
	}
	snapshot := r.ActiveCaptureRuns()
	if len(snapshot) != 1 || snapshot[0].ID != run.ID {
		t.Fatalf("active registry: %+v", snapshot)
	}
	snapshot[0].ID = "changed"
	if r.CaptureRunForUpstream("agent") != run.ID {
		t.Fatal("snapshot mutated the registry")
	}
	if err := r.StopCaptureRun(run.ID); err != nil {
		t.Fatal(err)
	}
	if len(r.ActiveCaptureRuns()) != 0 {
		t.Fatal("stopped run remained active")
	}
}
