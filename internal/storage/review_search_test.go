package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestReviewSearchScopesBeforePagination(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	ids := []string{}
	for i := 0; i < 12; i++ {
		entry := newTestLog(fmt.Sprintf("snapshot-%02d", i), time.Unix(1000+int64(i), 0))
		entry.Method, entry.Path, entry.ResponseBody = "POST", "/v1/messages", "needle 100%"
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
		if i >= 8 {
			ids = append(ids, entry.ID)
		}
	}
	// Earlier matching logs outside the snapshot must not consume page slots.
	repo := &AsyncRepository{inner: r}
	for offset := 0; offset < 4; offset += 2 {
		rows, err := repo.SearchReviewLogs(context.Background(), ids, "100%", false, 2, offset)
		if err != nil || len(rows) != 2 || rows[0].ID != ids[offset] || rows[1].ID != ids[offset+1] {
			t.Fatalf("snapshot page %d: %+v %v", offset, rows, err)
		}
	}
	rows, err := repo.SearchReviewLogs(context.Background(), nil, "needle", false, 20, 0)
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty scope searched live logs: %+v %v", rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = repo.SearchReviewLogs(ctx, ids, "needle", false, 20, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
