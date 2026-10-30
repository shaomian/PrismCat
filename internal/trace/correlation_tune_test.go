package trace

import (
	"testing"
	"time"
)

func TestTuneAppliesMaxEntries(t *testing.T) {
	c := NewCorrelator()
	c.Tune(2, DefaultIdle)
	now := time.Now()
	for i := 0; i < 5; i++ {
		c.Observe(observation(string(rune('a'+i)), "a", rootRequest, toolResponse, now.Add(time.Duration(i)*time.Second)))
	}
	if got := len(c.nodes); got != 2 {
		t.Fatalf("maxEntries ignored: nodes=%d, want 2", got)
	}
}

func TestTuneAppliesIdleWindow(t *testing.T) {
	c := NewCorrelator()
	c.Tune(DefaultMaxEntries, time.Minute)
	now := time.Now()
	first := c.Observe(observation("1", "a", rootRequest, toolResponse, now))[0]
	// 第二条观测晚于空闲窗口,第一条应被清理出内存索引
	c.Observe(observation("2", "a", rootRequest, toolResponse, now.Add(2*time.Minute)))
	if c.nodes[first.LogID] != nil {
		t.Fatalf("idle window ignored: stale observation %s still indexed", first.LogID)
	}
	if c.nodes["2"] == nil {
		t.Fatal("recent observation missing from index")
	}
}

func TestTuneIgnoresNonPositiveValues(t *testing.T) {
	c := NewCorrelator()
	c.Tune(0, 0)
	if c.maxEntries != DefaultMaxEntries || c.idle != DefaultIdle {
		t.Fatalf("non-positive Tune overwrote defaults: maxEntries=%d idle=%s", c.maxEntries, c.idle)
	}
	c.Tune(-1, -time.Minute)
	if c.maxEntries != DefaultMaxEntries || c.idle != DefaultIdle {
		t.Fatalf("negative Tune overwrote defaults: maxEntries=%d idle=%s", c.maxEntries, c.idle)
	}
}
