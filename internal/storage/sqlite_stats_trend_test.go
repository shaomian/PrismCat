package storage

import (
	"testing"
	"time"
)

func TestGetStatsTrendCoversWindowAndCountsErrors(t *testing.T) {
	repo := mustNewSQLiteRepoForTest(t)
	defer repo.Close()

	now := time.Now().UTC()
	cases := []struct {
		id        string
		createdAt time.Time
		status    int
	}{
		{"old-ok", now.Add(-2 * time.Hour), 200},
		{"mid-err", now.Add(-1 * time.Hour), 500},
		{"now-ok", now, 200},
	}
	for _, c := range cases {
		log := newTestLog(c.id, c.createdAt)
		log.StatusCode = c.status
		if err := repo.SaveLog(log); err != nil {
			t.Fatalf("save log %s: %v", c.id, err)
		}
	}

	stats, err := repo.GetStats(nil)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if len(stats.Trend) != statsTrendBuckets {
		t.Fatalf("trend buckets = %d, want %d", len(stats.Trend), statsTrendBuckets)
	}

	var total, errs int64
	errorBuckets := 0
	for _, point := range stats.Trend {
		total += point.Total
		errs += point.Errors
		if point.Errors > 0 {
			errorBuckets++
		}
	}
	if total != 3 {
		t.Fatalf("trend total = %d, want 3", total)
	}
	if errs != 1 || errorBuckets != 1 {
		t.Fatalf("trend errors = %d in %d buckets, want 1 in 1 bucket", errs, errorBuckets)
	}

	// 窗口从最早一条日志开始,桶按时间递增等距
	first := stats.Trend[0].StartUnixMs
	if first > now.Add(-2*time.Hour).UnixMilli() {
		t.Fatalf("first bucket starts at %d, after the oldest log", first)
	}
	if stats.TrendIntervalMs <= 0 {
		t.Fatalf("interval = %d, want > 0", stats.TrendIntervalMs)
	}
}

func TestGetStatsTrendEmptyDatabase(t *testing.T) {
	repo := mustNewSQLiteRepoForTest(t)
	defer repo.Close()

	stats, err := repo.GetStats(nil)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if len(stats.Trend) != 0 {
		t.Fatalf("trend length = %d, want 0 for empty database", len(stats.Trend))
	}
}
