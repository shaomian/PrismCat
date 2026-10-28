package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTraceOriginSplitIsReadOnlyAndIndependentOfMetadata(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	groupingLog(t, r, "source-1", "explicit-source", true)
	groupingLog(t, r, "source-2", "explicit-source", true)
	groupingLog(t, r, "source-3", "explicit-source", true)
	result := applyGrouping(t, r, TraceGroupingRequest{Action: "split", TraceIDs: []string{"explicit-source"}, LogIDs: []string{"source-1", "source-2"}})
	m, err := r.GetTraceMetadataDocument(result.Target)
	if err != nil || m.Origin == nil || m.Origin.Kind != "manual_split" || m.Origin.RequestCount != 2 || !reflect.DeepEqual(m.Origin.SourceTraceIDs, []string{"explicit-source"}) || m.Origin.CreatedAt <= 0 {
		t.Fatalf("split provenance missing: %+v %v", m.Origin, err)
	}
	original := *m.Origin
	items, total, err := r.ListTraces(TraceFilter{TraceID: result.Target, Limit: 1})
	if err != nil || total != 1 || len(items) != 1 || !reflect.DeepEqual(items[0].Origin, &original) || len(items[0].Sources) != 1 || items[0].Sources[0] != "explicit" {
		t.Fatalf("list lost provenance or assignment-source semantics: %+v %d %v", items, total, err)
	}
	// Editing labels cannot forge the source or remove the system badge.
	m.Name, m.Labels, m.Pinned = "Renamed task", []string{"coding"}, true
	m.Origin = &TraceOrigin{Kind: "manual_split", SourceTraceIDs: []string{"forged"}, RequestCount: 999, CreatedAt: 1}
	m, err = r.SaveTraceMetadata(m)
	if err != nil || !reflect.DeepEqual(m.Origin, &original) || len(m.Labels) != 1 || m.Labels[0] != "coding" {
		t.Fatalf("metadata changed provenance: %+v %v", m, err)
	}
	var storedOrigin sql.NullString
	if err = r.db.QueryRow("SELECT json_extract(document,'$.origin') FROM trace_metadata WHERE trace_id=?", result.Target).Scan(&storedOrigin); err != nil || storedOrigin.Valid {
		t.Fatal("system provenance was written into editable metadata", err)
	}
	for _, action := range []string{"archive", "restore"} {
		if _, err = r.ApplyTaskManagement(context.Background(), TaskManagementRequest{Kind: "trace", Action: action, Items: []TaskManagementItem{{ID: result.Target}}}); err != nil {
			t.Fatal(err)
		}
		m, err = r.GetTraceMetadataDocument(result.Target)
		if err != nil || !reflect.DeepEqual(m.Origin, &original) || m.Name != "Renamed task" || !m.Pinned {
			t.Fatalf("management altered origin: %+v %v", m, err)
		}
	}
	// Its count records creation, not the current number of requests.
	applyGrouping(t, r, TraceGroupingRequest{Action: "split", TraceIDs: []string{result.Target}, LogIDs: []string{"source-1"}})
	items, _, err = r.ListTraces(TraceFilter{TraceID: result.Target, Limit: 1})
	if err != nil || len(items) != 1 || items[0].RequestCount != 1 || !reflect.DeepEqual(items[0].Origin, &original) {
		t.Fatalf("later split rewrote creation origin: %+v %v", items, err)
	}
	source, err := r.GetTraceMetadataDocument("explicit-source")
	if err != nil || source.Origin != nil {
		t.Fatal("original source was incorrectly marked as a split", err)
	}
}

func TestTraceOriginHistoricalRecordsAndMergeDoNotPropagate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "historical.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	groupingLog(t, r, "a", "source", false)
	groupingLog(t, r, "b", "source", false)
	groupingLog(t, r, "c", "merge-target", false)
	groupingLog(t, r, "d", "manual-prefix-only", false)
	result := applyGrouping(t, r, TraceGroupingRequest{Action: "split", TraceIDs: []string{"source"}, LogIDs: []string{"a"}})
	// Existing split records predate the provenance index; opening the database
	// must discover them without rebuilding or reading request bodies.
	if _, err = r.db.Exec("DROP INDEX idx_trace_operation_origin"); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	items, _, err := r.ListTraces(TraceFilter{TraceID: result.Target, Limit: 1})
	if err != nil || len(items) != 1 || items[0].Origin == nil {
		t.Fatalf("historical split not discovered: %+v %v", items, err)
	}
	plain, err := r.GetTraceMetadataDocument("manual-prefix-only")
	if err != nil || plain.Origin != nil {
		t.Fatal("guessed provenance from an ID prefix", err)
	}
	plain.Origin = &TraceOrigin{Kind: "manual_split", SourceTraceIDs: []string{"fake"}, RequestCount: 1, CreatedAt: 1}
	plain, err = r.SaveTraceMetadata(plain)
	if err != nil || plain.Origin != nil {
		t.Fatal("client forged provenance without a split record", err)
	}
	applyGrouping(t, r, TraceGroupingRequest{Action: "merge", TraceIDs: []string{result.Target}, Target: "merge-target"})
	merged, err := r.GetTraceMetadataDocument("merge-target")
	if err != nil || merged.Origin != nil {
		t.Fatal("split origin propagated to an unrelated merge target", err)
	}
}
