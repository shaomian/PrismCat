package storage

import (
	"testing"
	"time"
)

func TestTraceCatalogLabelSearchAndExactFiltering(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	for _, row := range []struct{ id, trace string }{{"a1", "a"}, {"a2", "a"}, {"b1", "b"}, {"body-only", "body"}} {
		entry := newTestLog(row.id, time.Now())
		entry.TraceID = row.trace
		entry.RequestBody = "回归测试"
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []TraceMetadata{{TraceID: "a", Name: "Task A", Labels: []string{"回归测试", "漏洞验证", "quoted'\"label"}, Status: "todo"}, {TraceID: "b", Name: "Task B", Labels: []string{"回归测试扩展"}}} {
		if _, err := r.SaveTraceMetadata(m); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := r.ListTraces(TraceFilter{Query: "回归", Limit: 1})
	if err != nil || total != 2 || len(items) != 1 {
		t.Fatalf("label search/page count: %+v %d %v", items, total, err)
	}
	items, total, err = r.ListTraces(TraceFilter{Label: "回归测试"})
	if err != nil || total != 1 || len(items) != 1 || items[0].TraceID != "a" || items[0].RequestCount != 2 {
		t.Fatalf("exact label or complete aggregate lost: %+v %d %v", items, total, err)
	}
	items, total, err = r.ListTraces(TraceFilter{Query: "漏洞", Label: "回归测试"})
	if err != nil || total != 1 || len(items) != 1 || items[0].Metadata.Status != "todo" {
		t.Fatalf("combined label search or legacy status changed: %+v %d %v", items, total, err)
	}
	_, total, err = r.ListTraces(TraceFilter{Query: "unrelated", Label: "回归测试"})
	if err != nil || total != 0 {
		t.Fatalf("search and exact filter did not intersect: %d %v", total, err)
	}
	_, total, err = r.ListTraces(TraceFilter{Query: "quoted'\"label"})
	if err != nil || total != 1 {
		t.Fatalf("quoted label was not handled safely: %d %v", total, err)
	}
}
