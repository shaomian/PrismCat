package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/storage"
)

func (h *Handler) reviewMembers(ctx context.Context, snapshot string, tasks []benchmark.ReviewTask, run string, ids []string) (map[string]bool, error) {
	if snapshot != "" {
		store, ok := h.repo.(storage.ReviewSnapshotRepository)
		if !ok {
			return nil, errors.New("review snapshot unavailable")
		}
		return store.ReviewSnapshotMembers(ctx, snapshot, run, ids)
	}
	result := map[string]bool{}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	for _, task := range tasks {
		if run != "" && run != task.RunID {
			continue
		}
		for _, id := range task.Metrics.LogIDs {
			if wanted[id] {
				result[id] = true
			}
		}
	}
	return result, ctx.Err()
}

func (e *reviewExecutor) snapshotRequests(ctx context.Context, run string, cursor int, query string, errorsOnly bool) (string, error) {
	store, ok := e.h.repo.(storage.ReviewSnapshotRepository)
	if !ok {
		return "", errors.New("review snapshot unavailable")
	}
	rows, err := store.ReviewSnapshotRequests(ctx, e.snapshotID, run, cursor, strings.TrimSpace(query), errorsOnly, reviewSearchWindow)
	if err != nil {
		return "", err
	}
	total := 0
	for _, task := range e.tasks {
		if task.RunID == run {
			total = task.Metrics.Requests
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s has %d frozen model requests. Filter query=%q errors_only=%v. Keyword search covers saved previews, not all body content.\n", run, total, reviewClip(query, 200), errorsOnly)
	outputLimit := min(12000, e.bytes)
	if b.Len()+128 > outputLimit {
		return "", errors.New("Evidence budget reached. Report any remaining uncertainty.")
	}
	consumed, last := 0, max(cursor, 0)
	for _, row := range rows {
		if consumed >= 20 {
			break
		}
		var line strings.Builder
		if row.Missing {
			fmt.Fprintf(&line, "#%d log_id=%s unavailable\n", row.Position, row.Log.ID)
		} else {
			fmt.Fprintf(&line, "#%d log_id=%s %s %s status=%d latency=%dms", row.Position, row.Log.ID, row.Log.Method, reviewClip(row.Log.Path, 200), row.Log.StatusCode, row.Log.Latency)
			if query != "" {
				if match := requestMatchSnippet(query, row.Log); match != "" {
					fmt.Fprintf(&line, " match: %s", match)
				}
			}
			line.WriteByte('\n')
		}
		if b.Len()+line.Len()+128 > outputLimit {
			break
		}
		b.WriteString(line.String())
		consumed++
		last = row.Position
	}
	if len(rows) > 0 && consumed == 0 {
		return "", errors.New("Evidence budget reached. Report any remaining uncertainty.")
	}
	if consumed < len(rows) || len(rows) == reviewSearchWindow || ((strings.TrimSpace(query) == "" && !errorsOnly) && last < total && consumed > 0) {
		fmt.Fprintf(&b, "next_offset=%d\n", last)
	}
	return b.String(), ctx.Err()
}

func (h *Handler) normalizeSnapshotReviewOutput(ctx context.Context, output benchmark.ReviewOutput, report benchmark.ReviewReport, followup bool) (benchmark.ReviewOutput, error) {
	if report.SnapshotID == "" {
		return normalizeReviewOutput(output, report, followup)
	}
	// Validate only model-supplied candidate citations, never materialize the
	// complete directory. The model output itself has a 128 KiB bound.
	validation := report
	validation.Tasks = append([]benchmark.ReviewTask(nil), report.Tasks...)
	for i := range validation.Tasks {
		task := &validation.Tasks[i]
		candidates := []string{}
		for _, o := range output.Observations {
			for _, f := range o.Findings {
				if f.RunID == task.RunID {
					candidates = append(candidates, f.Evidence...)
				}
			}
		}
		allowed, err := h.reviewMembers(ctx, report.SnapshotID, report.Tasks, task.RunID, candidates)
		if err != nil {
			return output, err
		}
		task.Metrics.LogIDs = []string{}
		for id := range allowed {
			task.Metrics.LogIDs = append(task.Metrics.LogIDs, id)
		}
	}
	return normalizeReviewOutput(output, validation, followup)
}
