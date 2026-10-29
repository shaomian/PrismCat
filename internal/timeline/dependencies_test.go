package timeline

import (
	"github.com/shaomian/PrismCat/internal/trace"
	"strings"
	"testing"
)

func TestForkedTimelineUsesParentAndGraphDoesNotInventEdges(t *testing.T) {
	chain := openAIChain()
	chain[0].TraceAssociation = &trace.Assignment{Source: "auto"}
	chain[1].TraceAssociation = &trace.Assignment{Source: "auto", Parents: []string{"1"}}
	sibling := chain[1]
	sibling.LogID, sibling.Seq = "3", 3
	// The immediately previous sibling has an unrelated context.
	unrelated := Input{LogID: "unrelated", Seq: 9, Path: "/v1/responses", RequestBody: `{"input":"another task"}`, TraceAssociation: &trace.Assignment{Source: "capture"}}
	tl := Build("t", []Input{chain[0], chain[1], unrelated, sibling}, Options{})
	if tl.Requests[3].Carried != 3 {
		t.Fatalf("compared with sibling instead of parent: %+v", tl.Requests[3])
	}
	graph := Dependencies(tl)
	if !strings.Contains(graph, "n0 -.-> n1") || !strings.Contains(graph, "n0 -.-> n3") || strings.Count(graph, "-.->") != 2 {
		t.Fatalf("wrong graph: %s", graph)
	}
}

func TestDependencyPagePreservesParentsOutsideView(t *testing.T) {
	tl := &Timeline{Requests: []RequestEntry{{
		LogID: "child", Seq: 26,
		TraceAssociation: &trace.Assignment{Parents: []string{"parent", "parent"}},
	}}}
	graph := Dependencies(tl)
	if !strings.Contains(graph, "Request 26") || !strings.Contains(graph, "Parent outside this view") || strings.Count(graph, "e0 -.-> n0") != 1 {
		t.Fatalf("page lost or duplicated a parent: %s", graph)
	}
}
