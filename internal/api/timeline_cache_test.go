package api

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/timeline"
)

func TestTraceTimelineCacheHitInvalidationAndLRU(t *testing.T) {
	c := newTraceTimelineCache()
	tl := &timeline.Timeline{TraceID: "t1"}
	fp := storage.TraceFingerprint{Count: 2, LastMs: 10}

	c.put("t1", fp, tl)
	if got := c.get("t1", fp); got != tl {
		t.Fatal("expected cache hit")
	}

	// 指纹变化 → 失效并清除
	if got := c.get("t1", storage.TraceFingerprint{Count: 3, LastMs: 10}); got != nil {
		t.Fatal("expected miss on changed fingerprint")
	}
	if got := c.get("t1", fp); got != nil {
		t.Fatal("stale entry should be dropped after fingerprint mismatch")
	}

	// LRU 容量上限：最早的条目被淘汰
	for i := 0; i < tlCacheLimit+5; i++ {
		c.put(fmt.Sprintf("t%d", i), fp, tl)
	}
	c.mu.Lock()
	size := len(c.entries)
	_, old := c.entries["t1"]
	c.mu.Unlock()
	if size != tlCacheLimit {
		t.Fatalf("cache size = %d, want %d", size, tlCacheLimit)
	}
	if old {
		t.Fatal("oldest entries should have been evicted")
	}
	if c.get(fmt.Sprintf("t%d", tlCacheLimit+4), fp) == nil {
		t.Fatal("newest entry should be present")
	}
}

func TestTraceTimelineColdBuildsShareCache(t *testing.T) {
	c := newTraceTimelineCache()
	fp := storage.TraceFingerprint{Count: 1}
	var builds atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			release, err := c.lockBuild(context.Background(), "one")
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			if c.get("one", fp) == nil {
				builds.Add(1)
				c.put("one", fp, &timeline.Timeline{TraceID: "one"})
			}
		})
	}
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("cold builds = %d, want one", builds.Load())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.building) != 0 {
		t.Fatal("completed build retained a lock")
	}
}

func TestTraceTimelineBuildWaitCancellationAndIndependence(t *testing.T) {
	c := newTraceTimelineCache()
	release, err := c.lockBuild(context.Background(), "one")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	other, err := c.lockBuild(context.Background(), "two")
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		unlock, err := c.lockBuild(ctx, "one")
		if unlock != nil {
			unlock()
		}
		result <- err
	}()
	cancel()
	if err := <-result; err != context.Canceled {
		t.Fatalf("waiting request returned %v", err)
	}
}

func TestDeriveTimelineTruncationLevels(t *testing.T) {
	long := strings.Repeat("x", 5000)
	full := timeline.Build("d", []timeline.Input{{
		LogID:        "1",
		Path:         "/v1/chat/completions",
		RequestBody:  `{"messages":[{"role":"user","content":"` + long + `"}]}`,
		ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"` + long + `"}}]}`,
	}}, timeline.Options{MaxEventChars: math.MaxInt})

	// 默认级别（Options{} → 2000）：与直接构建等价
	def := deriveTimeline(full, timeline.Options{})
	if ev := def.Requests[0].NewEvents[0]; !ev.Truncated || len([]rune(ev.Content)) != timeline.DefaultEventChars+1 {
		t.Fatalf("default derivation = %d runes, truncated=%v", len([]rune(ev.Content)), ev.Truncated)
	}

	// 不限级别：原样返回完整构建
	if got := deriveTimeline(full, timeline.Options{MaxEventChars: math.MaxInt}); got != full {
		t.Fatal("unlimited derivation should return the full build")
	}

	// 显式级别
	small := deriveTimeline(full, timeline.Options{MaxEventChars: 40})
	if ev := small.Requests[0].NewEvents[0]; !ev.Truncated || len([]rune(ev.Content)) != 41 {
		t.Fatalf("small derivation = %d runes", len([]rune(ev.Content)))
	}
}
