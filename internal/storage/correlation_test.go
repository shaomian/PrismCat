package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/trace"
)

func TestAssignmentSurvivesStaleSnapshotsAndInvalidatesCache(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	entry := newTestLog("auto-log", time.Now())
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	a, err := r.AssignTrace(entry.ID, "group-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	before, err := r.GetTraceFingerprint("group-a")
	if err != nil {
		t.Fatal(err)
	}
	entry.ResponseBody = "finished"
	entry.StatusCode = 200
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetLog(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TraceID != "group-a" || got.TraceAssociation == nil || !got.TraceAssociation.Locked || got.ResponseBody != "finished" {
		t.Fatalf("stale snapshot reverted assignment: %+v", got)
	}
	after, _ := r.GetTraceFingerprint("group-a")
	if after.Revision <= before.Revision {
		t.Fatal("body update did not invalidate cache")
	}
	if _, err := r.AssignTrace(entry.ID, "group-b", 0); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatalf("stale correction accepted: %v", err)
	}
	if _, err := r.AssignTrace(entry.ID, "group-b", a.Revision); err != nil {
		t.Fatal(err)
	}
	old, _ := r.GetTraceFingerprint("group-a")
	next, _ := r.GetTraceFingerprint("group-b")
	if old.Count != 0 || next.Count != 1 || old.Revision <= after.Revision {
		t.Fatalf("stale group counts: %+v %+v", old, next)
	}
}

func TestExplicitTraceCannotBeAutomaticallyOrManuallyReassigned(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	entry := newTestLog("explicit", time.Now())
	entry.TraceID = "client-task"
	entry.TraceSeq = 7
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AssignTrace(entry.ID, "other", 0); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatalf("explicit trace overwritten: %v", err)
	}
	got, _ := r.GetLog(entry.ID)
	if got.TraceID != "client-task" || got.TraceSeq != 7 || got.TraceAssociation.Source != "explicit" {
		t.Fatalf("explicit trace changed: %+v", got)
	}
}

func TestMixedExplicitAndAutomaticSequenceRemainsStable(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	now := time.Now()
	explicit := newTestLog("explicit-first", now)
	explicit.TraceID = "client-task"
	explicit.TraceSeq = 1
	if err := r.SaveLog(explicit); err != nil {
		t.Fatal(err)
	}
	o := trace.Observation{LogID: explicit.ID, Scope: "a", CreatedAt: now, Assignment: trace.Assignment{TraceID: "client-task", SessionID: "s", Source: "explicit", State: "declared", Reason: "explicit_header"}}
	if _, err := r.storeObservation(o, false); err != nil {
		t.Fatal(err)
	}
	auto := newTestLog("auto-next", now.Add(time.Second))
	if err := r.SaveLog(auto); err != nil {
		t.Fatal(err)
	}
	o.LogID = auto.ID
	o.Assignment.Source = "auto"
	if _, err := r.storeObservation(o, false); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveLog(explicit); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveLog(auto); err != nil {
		t.Fatal(err)
	}
	logs, err := r.GetTraceRequests("client-task")
	if err != nil || len(logs) != 2 || logs[0].TraceSeq != 1 || logs[1].TraceSeq != 2 {
		t.Fatalf("mixed sequence corrupted: %+v %v", logs, err)
	}
}

func TestCaptureFreezeAndSingleTaskGrouping(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	run, err := r.StartCaptureRun("agent experiment", "agent-a", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartCaptureRun("competing", "agent-a", false); err == nil {
		t.Fatal("overlapping captures accepted")
	}
	frozen := r.CaptureRunForUpstream("agent-a")
	if err := r.StopCaptureRun(run.ID); err != nil {
		t.Fatal(err)
	}
	if r.CaptureRunForUpstream("agent-a") != "" {
		t.Fatal("new requests still captured")
	}
	entry := newTestLog("long-stream", time.Now())
	entry.CaptureRunID = frozen
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	logs, total, err := r.ListLogs(LogFilter{CaptureRunID: run.ID})
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("lost frozen membership: %v %d", err, total)
	}
	runs, err := r.ListCaptureRuns()
	if err != nil || len(runs) != 1 || runs[0].RequestCount != 1 || runs[0].StoppedAt == nil || runs[0].UsageMissing != 1 {
		t.Fatalf("bad capture stats: %+v %v", runs, err)
	}
	a, ok := r.captureAssignment(run.ID)
	if !ok || a.Source != "capture" {
		t.Fatal("stopped run lost single-task assignment")
	}
}

func TestPersistentSequenceAndFeatureRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := newTestLog("one", time.Now())
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	o := trace.Observation{LogID: entry.ID, Scope: "agent", CreatedAt: entry.CreatedAt, Request: trace.ParseRequest("/v1/responses", []byte(`{"input":"hello"}`)), Response: trace.ParseResponse("openai-responses", []byte(`{"id":"response-one","output":[]}`), false), Assignment: trace.Assignment{TraceID: "auto-one", SessionID: "session-one", Source: "auto", Reason: "new_task", State: "standalone"}}
	if _, err := r.storeObservation(o, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	observations, err := r.loadObservations(config.CorrelationConfig{}, time.Now())
	if err != nil || len(observations) != 1 || observations[0].Response.ResponseID != "response-one" {
		t.Fatalf("features not recovered: %+v %v", observations, err)
	}
	entry.ID = "two"
	entry.TraceID = ""
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	o.LogID = entry.ID
	if _, err := r.storeObservation(o, false); err != nil {
		t.Fatal(err)
	}
	logs, err := r.GetTraceRequests("auto-one")
	if err != nil || len(logs) != 2 || logs[0].TraceSeq == logs[1].TraceSeq {
		t.Fatalf("sequence lost on restart: %+v %v", logs, err)
	}
}

func TestAsyncCorrelationUsesOriginalBodyAndKeepsLogWriterIndependent(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	cfg := &config.Config{Correlation: config.CorrelationConfig{Enabled: true}}
	a := NewAsyncRepository(r, cfg, 16)
	now := time.Now()
	first := newTestLog("first", now)
	first.Path = "/v1/chat/completions"
	first.Method = "POST"
	first.Upstream = "agent"
	first.RequestHeaders = map[string][]string{"Content-Type": {"application/json"}}
	first.ResponseHeaders = first.RequestHeaders
	first.StatusCode = 200
	first.CorrelationFinal = true
	first.RequestBodyRaw = []byte(`{"messages":[{"role":"user","content":"Fix login"}]}`)
	first.ResponseBodyRaw = []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"read","arguments":"{}"}}]}}]}`)
	second := first.Clone()
	second.ID = "second"
	second.CreatedAt = now.Add(time.Second)
	second.ResponseBodyRaw = nil
	second.RequestBodyRaw = []byte(`{"messages":[{"role":"user","content":"unrelated overwritten body"}]}`)
	second.RequestBodyOriginalRaw = []byte(`{"messages":[{"role":"user","content":"Fix login"},{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"file"}]}`)
	if err := a.SaveLog(first); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveLog(second); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		one, e1 := r.GetLog("first")
		two, e2 := r.GetLog("second")
		if e1 == nil && e2 == nil && one.TraceID != "" && one.TraceID == two.TraceID {
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer a.Close()
	t.Fatal("async correlation did not join tool result using original request")
}
