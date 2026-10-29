package storage

import (
	"fmt"
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

func TestGetStatsTrendLastBucketPreservesAllRows(t *testing.T) {
	repo := mustNewSQLiteRepoForTest(t)
	defer repo.Close()
	// 保证窗口结束点由最大日志时间决定，覆盖刚好落在第 24 桶边界的记录。
	start := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	for i, offset := range []time.Duration{0, 23 * time.Millisecond, 24 * time.Millisecond} {
		log := newTestLog(fmt.Sprint(i), start.Add(offset))
		log.StatusCode = 500
		if err := repo.SaveLog(log); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := repo.GetStats(nil)
	if err != nil {
		t.Fatal(err)
	}
	var total, errors int64
	for _, point := range stats.Trend {
		total += point.Total
		errors += point.Errors
	}
	if total != stats.TotalRequests || errors != stats.ErrorCount {
		t.Fatalf("trend totals %d/%d differ from summary %d/%d", total, errors, stats.TotalRequests, stats.ErrorCount)
	}
}

func TestGetStatsSinceAndGroupedMetrics(t *testing.T) {
	repo := mustNewSQLiteRepoForTest(t)
	defer repo.Close()
	now := time.Now().UTC()
	since := now.Add(-time.Hour)
	for i, data := range []struct {
		upstream  string
		status    int
		latency   int64
		streaming bool
		error     string
		at        time.Time
	}{
		{"a", 200, 100, true, "", now},
		{"b", 200, 300, false, "disconnected", now},
		{"a", 503, 800, true, "", now},
		{"a", 400, 5000, false, "", since.Add(-time.Millisecond)},
	} {
		log := newTestLog(fmt.Sprint(i), data.at)
		log.Upstream, log.StatusCode, log.Latency, log.Streaming, log.Error = data.upstream, data.status, data.latency, data.streaming, data.error
		if err := repo.SaveLog(log); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := repo.GetStats(&since)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != 3 || stats.SuccessCount != 2 || stats.ErrorCount != 2 || stats.StreamingCount != 2 || stats.AvgLatency != 400 {
		t.Fatalf("unexpected summary: %+v", stats)
	}
	if stats.ByUpstream["a"] != 2 || stats.ByUpstream["b"] != 1 || stats.ByStatusCode[200] != 2 || stats.ByStatusCode[503] != 1 || len(stats.ByStatusCode) != 2 {
		t.Fatalf("unexpected groups: %+v", stats)
	}
	if stats.Trend[0].StartUnixMs != since.UnixMilli() {
		t.Fatal("trend must start at the requested window")
	}
	var total, errors int64
	for _, point := range stats.Trend {
		total += point.Total
		errors += point.Errors
	}
	if total != 3 || errors != 2 {
		t.Fatalf("unexpected trend totals: %d/%d", total, errors)
	}
}
