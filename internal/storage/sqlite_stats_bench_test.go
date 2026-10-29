package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func BenchmarkGetStats(b *testing.B) {
	for _, count := range []int{10000, 50000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			repo, err := NewSQLiteRepository(filepath.Join(b.TempDir(), "stats.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = repo.Close() })
			_, err = repo.db.Exec(`
				WITH RECURSIVE entries(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM entries WHERE n+1 < ?)
				INSERT INTO request_logs (id, created_at, created_at_unix_ms, upstream, target_url, method, path,
					status_code, error, streaming, latency_ms, request_body)
				SELECT printf('log-%d', n), ?, ? + n * 1000, printf('upstream-%d', n % 4), 'http://example.test',
					'POST', '/v1/responses', CASE WHEN n % 10 = 0 THEN 500 ELSE 200 END, '', n % 2, 100 + n % 100, ?
				FROM entries`, count, time.Now().UTC().Format(time.RFC3339Nano), time.Now().Add(-48*time.Hour).UnixMilli(), strings.Repeat("x", 2048))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := repo.GetStats(nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
