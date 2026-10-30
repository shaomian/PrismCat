package storage

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/trace"
)

const recoveryRequest = `{"messages":[{"role":"user","content":"task"}]}`
const recoveryResponse = `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call","function":{"name":"read","arguments":"{}"}}]}}]}`
const recoveryContinuation = `{"messages":[{"role":"user","content":"task"},{"role":"assistant","tool_calls":[{"id":"call","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call","content":"ok"}]}`

func recoveryObservation(id string, created, observed time.Time, request, response string) trace.Observation {
	return trace.Observation{LogID: id, Scope: "agent", CreatedAt: created, ObservedAt: observed,
		RootTraceID: "root", RootSessionID: "session",
		Request:    trace.ParseRequest("/v1/chat/completions", []byte(request)),
		Response:   trace.ParseResponse("openai-chat", []byte(response), false),
		Assignment: trace.Assignment{TraceID: "root", SessionID: "session", Source: "auto", State: "standalone"}}
}

func saveRecoveryLog(t *testing.T, r *SQLiteRepository, id string, at time.Time) *RequestLog {
	t.Helper()
	entry := newTestLog(id, at)
	entry.Upstream = "agent"
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestCorrelationRecoveryUsesConfiguredWindow(t *testing.T) {
	for _, minutes := range []int{0, 10, 60} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			r := mustNewSQLiteRepoForTest(t)
			now := time.Now()
			at := now.Add(-45 * time.Minute)
			saveRecoveryLog(t, r, "parent", at)
			if _, err := r.storeObservation(recoveryObservation("parent", at, at, recoveryRequest, recoveryResponse), false); err != nil {
				t.Fatal(err)
			}
			a := NewAsyncRepository(r, &config.Config{Correlation: config.CorrelationConfig{Enabled: true, MaxEntries: 4096, IdleMinutes: minutes}}, 16)
			defer a.Close()
			entry := saveRecoveryLog(t, r, "child", now)
			o := recoveryObservation("child", now, now, recoveryContinuation, "")
			o.Assignment = trace.Assignment{}
			o.RootTraceID, o.RootSessionID = "", ""
			a.processCorrelation(&o, entry)
			got, err := r.GetLog(entry.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (got.TraceID == "root") != (minutes == 60) {
				t.Fatalf("window %d: %+v", minutes, got.TraceAssociation)
			}
		})
	}
}

func TestCorrelationRecoveryRespectsCapacityAndLegacyActivity(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	now := time.Now().Truncate(time.Millisecond)
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare("INSERT INTO trace_assignments(log_id,trace_id,trace_seq,info,features,updated_at) VALUES(?,'root',1,?,?,?)")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2060 {
		o := recoveryObservation(fmt.Sprintf("entry-%04d", i), now.Add(-2*time.Hour), time.Time{}, recoveryRequest, recoveryResponse)
		features, _ := json.Marshal(o)
		info, _ := json.Marshal(o.Assignment)
		if _, err := statement.Exec(o.LogID, string(info), string(features), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	statement.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 16, 4096} {
		observations, err := r.loadObservations(config.CorrelationConfig{MaxEntries: limit}, now)
		if err != nil {
			t.Fatal(err)
		}
		want := min(config.NormalizeCorrelation(config.CorrelationConfig{MaxEntries: limit}).MaxEntries, 2060)
		if len(observations) != want || observations[0].LogID != "entry-2059" || !observations[0].ActiveAt().Equal(now) {
			t.Fatalf("limit %d: len=%d, first=%+v", limit, len(observations), observations[0])
		}
	}
}

func TestLateFinalResponseAfterRestartKeepsPersistedIdentity(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	now := time.Now()
	at := now.Add(-2 * time.Hour)
	parent := saveRecoveryLog(t, r, "parent", at)
	o := recoveryObservation(parent.ID, at, at, recoveryRequest, "")
	o.Assignment.TraceID, o.Assignment.State = "confirmed-task", "inferred"
	if _, err := r.storeObservation(o, false); err != nil {
		t.Fatal(err)
	}
	// Simulate feature expiry while the response is still in flight.
	if _, err := r.db.Exec("UPDATE trace_assignments SET features='' WHERE log_id=?", parent.ID); err != nil {
		t.Fatal(err)
	}
	a := NewAsyncRepository(r, &config.Config{Correlation: config.CorrelationConfig{Enabled: true, IdleMinutes: 1}}, 16)
	defer a.Close()
	final := recoveryObservation(parent.ID, at, now, recoveryRequest, recoveryResponse)
	final.Assignment = trace.Assignment{}
	final.RootTraceID, final.RootSessionID = "", ""
	a.processCorrelation(&final, parent)
	// Reload as on restart: the completed response must now be fresh evidence.
	a.reloadCorrelator(r)
	child := saveRecoveryLog(t, r, "child", now)
	continuation := recoveryObservation(child.ID, now, now, recoveryContinuation, "")
	continuation.Assignment = trace.Assignment{}
	continuation.RootTraceID, continuation.RootSessionID = "", ""
	a.processCorrelation(&continuation, child)
	for _, id := range []string{parent.ID, child.ID} {
		got, err := r.GetLog(id)
		if err != nil || got.TraceID != "confirmed-task" {
			t.Fatalf("lost late response identity for %s: %+v %v", id, got, err)
		}
	}
}

func TestAssignmentUpdatesDoNotRefreshEvidenceAge(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	at := time.Now().Add(-5 * time.Minute).Truncate(time.Millisecond)
	saveRecoveryLog(t, r, "parent", at)
	o := recoveryObservation("parent", at, at, recoveryRequest, recoveryResponse)
	if _, err := r.storeObservation(o, false); err != nil {
		t.Fatal(err)
	}
	o.Assignment.TraceID = "updated-task"
	assignment, err := r.storeObservation(o, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AssignTrace(o.LogID, "manual-task", assignment.Revision); err != nil {
		t.Fatal(err)
	}
	entry, err := r.GetLog(o.LogID)
	if err != nil {
		t.Fatal(err)
	}
	entry.StatusCode = 200
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	applyGrouping(t, r, TraceGroupingRequest{Action: "split", TraceIDs: []string{"manual-task"}, LogIDs: []string{o.LogID}})
	var updatedAt int64
	if err := r.db.QueryRow("SELECT updated_at FROM trace_assignments WHERE log_id=?", o.LogID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt != at.UnixMilli() {
		t.Fatalf("grouping refreshed old evidence: got %d want %d", updatedAt, at.UnixMilli())
	}
}
