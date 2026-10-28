package storage

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTraceTopOrderingBeforePaginationAndAfterRefresh(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	now := time.Now()
	for i, id := range []string{"old-a", "old-b", "recent"} {
		entry := newTestLog(id, now.Add(time.Duration(i-3)*time.Hour))
		entry.TraceID, entry.StatusCode = id, 200
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	a, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "old-a", TopAt: 1, Pinned: true})
	if err != nil || a.TopAt < now.UnixMilli() {
		t.Fatalf("pin did not receive server time: %+v %v", a, err)
	}
	// An earlier persisted pin precedes today's pin, irrespective of log age.
	if _, err = r.db.Exec(`UPDATE trace_metadata SET document=json_set(document,'$.top_at',1) WHERE trace_id='old-a'`); err != nil {
		t.Fatal(err)
	}
	b, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "old-b", TopAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	notArchived := false
	check := func(offset int, want string) {
		t.Helper()
		items, total, err := r.ListTraces(TraceFilter{Archived: &notArchived, Limit: 1, Offset: offset})
		if err != nil || total != 3 || len(items) != 1 || items[0].TraceID != want {
			t.Fatalf("page %d: want %s, got %+v total=%d err=%v", offset, want, items, total, err)
		}
	}
	check(0, "old-b")
	check(1, "old-a")
	check(2, "recent")
	entry := newTestLog("later-a", now.Add(time.Hour))
	entry.TraceID, entry.StatusCode = "old-a", 200
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	check(0, "old-b")
	check(1, "old-a")
	items, _, err := r.ListTraces(TraceFilter{Query: "recent"})
	if err != nil || len(items) != 1 || items[0].TraceID != "recent" {
		t.Fatalf("pin escaped search filter: %+v %v", items, err)
	}
	cutoff := now.Add(-90 * time.Minute)
	items, _, err = r.ListTraces(TraceFilter{StartTime: &cutoff})
	if err != nil || len(items) != 2 || items[0].TraceID != "old-a" || items[1].TraceID != "recent" {
		t.Fatalf("pin escaped time filter: %+v %v", items, err)
	}
	// Other edits preserve the original top position, and stale edits conflict.
	stale := b
	originalTop := b.TopAt
	b.Name, b.TopAt = "Edited", 99
	b, err = r.SaveTraceMetadata(b)
	if err != nil || b.TopAt != originalTop {
		t.Fatalf("edit moved top position: %+v %v", b, err)
	}
	if _, err = r.SaveTraceMetadata(stale); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatalf("stale pin accepted: %v", err)
	}
	b.TopAt = 0
	if _, err = r.SaveTraceMetadata(b); err != nil {
		t.Fatal(err)
	}
	check(0, "old-a")
	check(1, "recent")
	check(2, "old-b")
	// Default ordering uses the summary index instead of a whole-list temp sort.
	rows, err := r.db.Query(`EXPLAIN QUERY PLAN SELECT s.trace_id FROM trace_summaries s LEFT JOIN trace_metadata m ON m.trace_id=s.trace_id WHERE COALESCE(json_extract(m.document,'$.archived'),0)=0 ORDER BY s.top_at DESC,s.last_time DESC,s.trace_id LIMIT 20`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	if err = rows.Err(); err != nil || !strings.Contains(plan, "idx_trace_summary_top_recent") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("unindexed ordering: %s %v", plan, err)
	}
}

func TestTraceTopMigrationAndReopenPreserveFavorites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if r != nil {
			_ = r.Close()
		}
	})
	managementLog(t, r, "legacy-log", "legacy", "")
	m, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "legacy", Name: "Favorite", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.ListTraces(TraceFilter{}); err != nil {
		t.Fatal(err)
	}
	// Reproduce the existing summary schema and an old metadata document.
	if _, err = r.db.Exec(`DROP TRIGGER trace_metadata_top_insert; DROP TRIGGER trace_metadata_top_update; DROP TRIGGER trace_metadata_top_delete; DROP INDEX idx_trace_summary_top_recent; ALTER TABLE trace_summaries DROP COLUMN top_at`); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err = r.GetTraceMetadataDocument("legacy")
	if err != nil || !m.Pinned || m.TopAt != 0 || m.Name != "Favorite" {
		t.Fatalf("legacy favorite changed: %+v %v", m, err)
	}
	m.TopAt = 1
	m, err = r.SaveTraceMetadata(m)
	if err != nil || m.TopAt == 0 {
		t.Fatalf("could not pin migrated task: %+v %v", m, err)
	}
	expectedTop := m.TopAt
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := r.ListTraces(TraceFilter{})
	if err != nil || len(items) != 1 || items[0].Metadata.TopAt != expectedTop || !items[0].Metadata.Pinned {
		t.Fatalf("top position did not persist: %+v %v", items, err)
	}
	// Direct metadata saves obey the same archive/restore rule as bulk actions.
	m.Archived = true
	m, err = r.SaveTraceMetadata(m)
	if err != nil || m.TopAt != 0 || !m.Pinned {
		t.Fatalf("archive changed favorite or kept top: %+v %v", m, err)
	}
	m.Archived = false
	m, err = r.SaveTraceMetadata(m)
	if err != nil || m.TopAt != 0 || !m.Pinned {
		t.Fatalf("restore reinstated pin: %+v %v", m, err)
	}
}

func TestTraceTopDoesNotProtectLogsFromRetention(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	for _, id := range []string{"top-only", "favorite"} {
		entry := newTestLog(id, time.Now().Add(-72*time.Hour))
		entry.TraceID, entry.StatusCode = id, 200
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
		if _, err := r.SaveTraceMetadata(TraceMetadata{TraceID: id, TopAt: 1, Pinned: id == "favorite"}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.DeleteLogsBefore(time.Now()); err != nil || n != 1 {
		t.Fatalf("top changed retention: deleted=%d err=%v", n, err)
	}
	items, _, err := r.ListTraces(TraceFilter{})
	if err != nil || len(items) != 1 || items[0].TraceID != "favorite" {
		t.Fatalf("favorite protection changed: %+v %v", items, err)
	}
}
