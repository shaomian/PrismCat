package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/analysis"
	"github.com/shaomian/PrismCat/internal/config"
)

func TestTraceToolExecutorReadsRequestedBodyPart(t *testing.T) {
	repo, _, _ := reviewAPISeed(t)
	h := New(&config.Config{}, repo, nil, nil)
	exec := &traceToolExecutor{h: h, traceID: "a"}
	for _, tc := range []struct {
		part     string
		request  bool
		response bool
	}{
		{"request", true, false},
		{"response", false, true},
		{"both", true, true},
		{"", true, true},
		{" RESPONSE ", false, true},
	} {
		t.Run(tc.part, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"log_id": "a", "part": tc.part})
			out, err := exec.ExecuteToolCall(context.Background(), analysis.ToolGetRequestDetail, args)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "Find the sample result") != tc.request || strings.Contains(out, "This is recorded evidence") != tc.response {
				t.Fatalf("incorrect body part returned: %s", out)
			}
		})
	}
	if _, err := exec.ExecuteToolCall(context.Background(), analysis.ToolGetRequestDetail, json.RawMessage(`{"log_id":"a","part":"invalid"}`)); err == nil {
		t.Fatal("invalid body part silently returned metadata only")
	}
}
