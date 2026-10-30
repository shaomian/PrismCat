package trace

import (
	"testing"
	"time"
)

func TestExpiredEvidenceCannotMatchBeforeCleanup(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := c.Observe(observation("parent", "a", rootRequest, toolResponse, now))[0]
	c.Tune(16, time.Minute)
	child := c.Observe(observation("child", "a", resultRequest, "", now.Add(2*time.Minute)))[0]
	if child.Assignment.TraceID == first.Assignment.TraceID || child.Assignment.Reason != "pending_reference" {
		t.Fatalf("expired parent participated in matching: %+v", child.Assignment)
	}
}

func TestLateFinalResponseKeepsIdentityAndProvidesFreshEvidence(t *testing.T) {
	c := NewCorrelator()
	c.Tune(16, time.Minute)
	now := time.Now()
	initial := observation("parent", "a", rootRequest, "", now)
	first := c.Observe(initial)[0]
	final := observation("parent", "a", rootRequest, toolResponse, now)
	final.ObservedAt = now.Add(2 * time.Minute)
	updated := c.Observe(final)[0]
	child := observation("child", "a", resultRequest, "", now.Add(2*time.Minute+time.Second))
	got := c.Observe(child)[0]
	if updated.Assignment.TraceID != first.Assignment.TraceID || got.Assignment.TraceID != first.Assignment.TraceID {
		t.Fatalf("late completion lost membership: initial=%+v final=%+v child=%+v", first.Assignment, updated.Assignment, got.Assignment)
	}
	if c.nodes["parent"] == nil {
		t.Fatal("recent response expired according to request start time")
	}
}

func TestTuneAndRestoreApplyBoundsBeforeNextMatch(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	for i, id := range []string{"a", "b", "c"} {
		c.Restore(observation(id, "a", rootRequest, toolResponse, now.Add(time.Duration(i)*time.Second)))
	}
	c.Prune(now.Add(3 * time.Second))
	c.Tune(1, time.Minute)
	if len(c.nodes) != 1 || c.nodes["c"] == nil {
		t.Fatalf("capacity shrink not immediate: %+v", c.nodes)
	}
	c.Prune(now.Add(2 * time.Minute))
	c.Restore(observation("old", "a", rootRequest, toolResponse, now))
	child := c.Observe(observation("child", "a", resultRequest, "", now.Add(time.Second)))[0]
	if len(c.nodes) != 0 || child.Assignment.Reason != "pending_reference" {
		t.Fatalf("late observation moved clock backwards: nodes=%d assignment=%+v", len(c.nodes), child.Assignment)
	}
}

func TestRestoredEventBudgetUsesSameLimit(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	for i, id := range []string{"old", "new"} {
		o := observation(id, "a", rootRequest, "", now.Add(time.Duration(i)*time.Second))
		o.Request.Events = make([]Message, MaxEvents/2+1)
		c.Restore(o)
	}
	c.Prune(now.Add(2 * time.Second))
	if len(c.nodes) != 1 || c.nodes["new"] == nil {
		t.Fatal("restored message features exceeded the budget")
	}
}
