package api

import (
	"container/list"
	"math"
	"sync"

	"github.com/paopaoandlingyia/PrismCat/internal/storage"
	"github.com/paopaoandlingyia/PrismCat/internal/timeline"
)

// tlCacheLimit 缓存的调用链时间线上限。
// 完整构建（不截断）持有全部恢复后的 body，条目数要克制；
// LRU 淘汰保证活跃调用链的热路径命中。
const tlCacheLimit = 16

type tlCacheEntry struct {
	traceID string
	fp      storage.TraceFingerprint
	full    *timeline.Timeline // 不截断的完整构建
	elem    *list.Element
}

// traceTimelineCache 按 traceID 缓存完整构建的时间线。
// 读取前用 GetTraceFingerprint 做一次低成本查询判断失效
// （request_logs 只有插入没有在位内容更新，条数+最新创建时间即可覆盖），
// 命中后用 timeline.TruncateEvents 派生各种截断级别，
// blob 恢复与 JSON 解析对每条调用链只发生一次。
type traceTimelineCache struct {
	mu      sync.Mutex
	entries map[string]*tlCacheEntry
	lru     *list.List // front = 最近使用
}

func newTraceTimelineCache() *traceTimelineCache {
	return &traceTimelineCache{
		entries: make(map[string]*tlCacheEntry),
		lru:     list.New(),
	}
}

func (c *traceTimelineCache) get(traceID string, fp storage.TraceFingerprint) *timeline.Timeline {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[traceID]
	if !ok {
		return nil
	}
	if entry.fp != fp {
		c.removeLocked(entry)
		return nil
	}
	c.lru.MoveToFront(entry.elem)
	return entry.full
}

func (c *traceTimelineCache) put(traceID string, fp storage.TraceFingerprint, full *timeline.Timeline) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[traceID]; ok {
		entry.fp = fp
		entry.full = full
		c.lru.MoveToFront(entry.elem)
		return
	}
	entry := &tlCacheEntry{traceID: traceID, fp: fp, full: full}
	entry.elem = c.lru.PushFront(entry)
	c.entries[traceID] = entry
	for c.lru.Len() > tlCacheLimit {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.removeLocked(oldest.Value.(*tlCacheEntry))
	}
}

func (c *traceTimelineCache) removeLocked(entry *tlCacheEntry) {
	c.lru.Remove(entry.elem)
	delete(c.entries, entry.traceID)
	entry.full = nil
	entry.elem = nil
}

// deriveTimeline 按构建选项从完整时间线派生截断版本。
//   - MaxEventChars <= 0：使用默认 2000
//   - 极大值（不限截断）：直接返回完整构建（调用方只读）
//   - 其他：深拷贝 + 截断，等价于 Build(items, Options{MaxEventChars: n})
func deriveTimeline(full *timeline.Timeline, opts timeline.Options) *timeline.Timeline {
	maxChars := opts.MaxEventChars
	if maxChars <= 0 {
		maxChars = timeline.DefaultEventChars
	}
	if maxChars >= math.MaxInt {
		return full
	}
	return timeline.TruncateEvents(full, maxChars)
}
