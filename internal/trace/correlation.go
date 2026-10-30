package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Assignment struct {
	TraceID   string   `json:"trace_id"`
	SessionID string   `json:"session_id,omitempty"`
	Source    string   `json:"source"`
	Reason    string   `json:"reason"`
	State     string   `json:"state"`
	Parents   []string `json:"parents,omitempty"`
	Locked    bool     `json:"locked"`
	Revision  int64    `json:"revision"`
	Client    string   `json:"client,omitempty"`
}

type Observation struct {
	RootTraceID   string     `json:"root_trace_id"`
	RootSessionID string     `json:"root_session_id"`
	LogID         string     `json:"log_id"`
	Scope         string     `json:"scope"`
	CreatedAt     time.Time  `json:"created_at"`
	Request       Features   `json:"request"`
	Response      Features   `json:"response"`
	Assignment    Assignment `json:"assignment"`
}

// Correlator is owned by the repository's serialized correlation worker.
// Indices are bounded; their expiry never implies semantic task completion.
type Correlator struct {
	nodes      map[string]*Observation
	index      map[string]map[string]bool
	waiters    map[string]map[string]bool
	maxEntries int
	idle       time.Duration
}

// 索引边界的默认值。config 包负责校验和归一化(见 NormalizeCorrelation),
// 两处的默认数字必须保持一致。
const (
	DefaultMaxEntries = 2048
	DefaultIdle       = 30 * time.Minute
)

func NewCorrelator() *Correlator {
	return &Correlator{nodes: map[string]*Observation{}, index: map[string]map[string]bool{}, waiters: map[string]map[string]bool{}, maxEntries: DefaultMaxEntries, idle: DefaultIdle}
}

// Tune 调整索引边界,供配置热更新复用。非正值忽略,保持当前值。
func (c *Correlator) Tune(maxEntries int, idle time.Duration) {
	if maxEntries > 0 {
		c.maxEntries = maxEntries
	}
	if idle > 0 {
		c.idle = idle
	}
}

func domain(o *Observation) string { return o.Scope + "\x00" + o.Request.Format }

func key(scope, kind, value string) string { return digest([]string{scope, kind, value}) }
func prefixKeys(scope string, events []Message) []string {
	keys := make([]string, 0, len(events))
	previous := ""
	for _, event := range events {
		sum := sha256.Sum256([]byte(previous + "\x00" + event.Digest))
		previous = hex.EncodeToString(sum[:])
		keys = append(keys, key(scope, "context", previous))
	}
	return keys
}
func prefixKey(scope string, events []Message) string {
	keys := prefixKeys(scope, events)
	if len(keys) == 0 {
		return ""
	}
	return keys[len(keys)-1]
}

func (c *Correlator) addIndex(k, id string) {
	if c.index[k] == nil {
		c.index[k] = map[string]bool{}
	}
	c.index[k][id] = true
}

func (c *Correlator) produced(o *Observation) []string {
	if !o.Response.Valid {
		return nil
	}
	var keys []string
	if o.Request.Conversation != "" {
		keys = append(keys, key(domain(o), "conversation", o.Request.Conversation))
	}
	if o.Response.ResponseID != "" {
		keys = append(keys, key(domain(o), "response", o.Response.ResponseID))
	}
	for _, m := range o.Response.Events {
		if m.Kind == "tool_call" && m.CallID != "" {
			keys = append(keys, key(domain(o), "tool", m.CallID))
		}
	}
	if o.Request.Valid && len(o.Response.Events) > 0 {
		events := append(append([]Message{}, o.Request.Events...), o.Response.Events...)
		keys = append(keys, prefixKey(domain(o), events))
	}
	return keys
}

func (c *Correlator) dependencies(o *Observation) ([]string, string, int) {
	var previousKey string
	if o.Request.PreviousID != "" {
		previousKey = key(domain(o), "response", o.Request.PreviousID)
	}
	// Only the trailing tool-result frontier can establish tool continuation.
	var keys []string
	for i := len(o.Request.Events) - 1; i >= 0; i-- {
		m := o.Request.Events[i]
		if m.Kind != "tool_result" {
			break
		}
		if m.CallID != "" {
			keys = append(keys, key(domain(o), "tool", m.CallID))
		}
	}
	if previousKey != "" {
		return append(keys, previousKey), "previous_response", 0
	}
	if len(keys) > 0 {
		return keys, "tool_result", 0
	}
	prefixes := prefixKeys(domain(o), o.Request.Events)
	for n := len(prefixes); n > 0; n-- {
		k := prefixes[n-1]
		if len(c.index[k]) > 0 {
			return []string{k}, "context_continuation", n
		}
	}
	return nil, "no_match", 0
}

func (c *Correlator) decide(o *Observation) Assignment {
	a := o.Assignment
	if a.Locked || a.Source == "explicit" || a.Source == "capture" {
		return a
	}
	a = Assignment{TraceID: o.RootTraceID, SessionID: o.RootSessionID, Source: "auto", Reason: "new_task", State: "standalone", Revision: a.Revision}
	a.Client = o.Request.Client
	if !o.Request.Valid {
		a.Reason = "unsupported_or_incomplete"
		a.State = "unresolved"
		return a
	}
	keys, reason, prefix := c.dependencies(o)
	if o.Request.NativeTurn != "" {
		a = Assignment{TraceID: "auto-" + key(o.Scope, "native_turn", o.Request.NativeTurn), SessionID: o.RootSessionID, Source: "auto", Reason: "native_turn", State: "inferred", Client: o.Request.Client, Revision: a.Revision}
		seen := map[string]bool{}
		ambiguous := false
		for _, k := range keys {
			matches := 0
			for id := range c.index[k] {
				if p := c.nodes[id]; p != nil && id != o.LogID && p.CreatedAt.Before(o.CreatedAt) && p.Assignment.TraceID == a.TraceID {
					seen[id] = true
					matches++
				}
			}
			if matches > 1 {
				ambiguous = true
			}
		}
		// Native turn identity establishes membership, not execution order.
		if !ambiguous && (reason != "context_continuation" || len(seen) == 1) {
			for id := range seen {
				a.Parents = append(a.Parents, id)
			}
			sort.Strings(a.Parents)
		}
		return a
	}
	parents := map[string]*Observation{}
	missing := false
	for _, k := range keys {
		if len(c.index[k]) == 0 {
			missing = true
		}
		for id := range c.index[k] {
			if id != o.LogID {
				if p := c.nodes[id]; p != nil && !p.CreatedAt.After(o.CreatedAt) && (o.Request.NativeSession == "" || p.Request.NativeSession == "" || o.Request.NativeSession == p.Request.NativeSession) {
					parents[id] = p
				}
			}
		}
	}
	if missing {
		a.TraceID = o.RootTraceID
		a.Parents = nil
		a.State = "unresolved"
		a.Reason = "pending_reference"
		return a
	}
	if len(parents) == 0 {
		if o.Request.Conversation != "" && o.Request.NativeSession == "" {
			session := ""
			ambiguous := false
			for id := range c.index[key(domain(o), "conversation", o.Request.Conversation)] {
				if p := c.nodes[id]; p != nil {
					if session != "" && session != p.Assignment.SessionID {
						ambiguous = true
					}
					session = p.Assignment.SessionID
				}
			}
			if !ambiguous && session != "" {
				a.SessionID = session
			}
		}
		return a
	}
	traces := map[string]bool{}
	sessions := map[string]bool{}
	for _, p := range parents {
		traces[p.Assignment.TraceID] = true
		sessions[p.Assignment.SessionID] = true
	}
	if len(traces) != 1 || len(sessions) != 1 {
		a.TraceID = o.RootTraceID
		a.SessionID = o.RootSessionID
		a.Parents = nil
		a.State = "unresolved"
		a.Reason = "ambiguous_candidates"
		return a
	}
	// Content alone must identify one observed response, not several identical replays.
	if reason == "context_continuation" && len(parents) != 1 {
		a.TraceID = o.RootTraceID
		a.SessionID = o.RootSessionID
		a.Parents = nil
		a.State = "unresolved"
		a.Reason = "ambiguous_candidates"
		return a
	}
	var parent *Observation
	ids := make([]string, 0, len(parents))
	for id, p := range parents {
		ids = append(ids, id)
		parent = p
	}
	sort.Strings(ids)
	newUser := false
	if reason == "previous_response" {
		events := o.Request.Events
		history := append(append([]Message{}, parent.Request.Events...), parent.Response.Events...)
		if len(history) <= len(events) && len(history) > 0 && prefixKey(domain(o), history) == prefixKey(domain(o), events[:len(history)]) {
			events = events[len(history):]
		}
		for _, m := range events {
			if m.Role == "user" && m.Kind != "tool_result" {
				newUser = true
			}
		}
	} else if reason == "tool_result" {
		known := map[string]bool{}
		for _, p := range parents {
			for _, m := range p.Request.Events {
				if m.Role == "user" {
					known[m.Digest] = true
				}
			}
		}
		for _, m := range o.Request.Events {
			if m.Role == "user" && !known[m.Digest] {
				newUser = true
			}
		}
	} else if reason == "context_continuation" {
		for _, m := range o.Request.Events[prefix:] {
			if m.Role == "user" && m.Kind != "tool_result" {
				newUser = true
			}
		}
		// A short generic assistant answer plus shared prompts is not enough.
		if len(parent.Response.Events) == 1 && parent.Response.Events[0].Kind == "message" && parent.Response.Events[0].Size < 64 {
			a.TraceID = o.RootTraceID
			a.SessionID = o.RootSessionID
			a.Parents = nil
			a.State = "unresolved"
			a.Reason = "insufficient_context"
			return a
		}
	}
	a.SessionID = parent.Assignment.SessionID
	if o.Request.NativeSession != "" {
		a.SessionID = o.RootSessionID
	}
	if newUser {
		a.TraceID = o.RootTraceID
		a.Reason = "new_user_instruction"
		a.State = "inferred"
		a.Parents = nil
		return a
	}
	a.TraceID = parent.Assignment.TraceID
	a.Reason = reason
	a.State = "inferred"
	a.Parents = ids
	return a
}

// Restore registers persisted, authoritative decisions without reclassifying them.
func (c *Correlator) Restore(o Observation) {
	if o.RootTraceID == "" {
		o.RootTraceID = "auto-" + uuid.NewString()
		o.RootSessionID = "session-" + uuid.NewString()
	}
	c.nodes[o.LogID] = &o
	for _, k := range c.produced(&o) {
		c.addIndex(k, o.LogID)
	}
}

// Observe returns this decision and any previously waiting requests now resolved.
func (c *Correlator) Observe(o Observation) []Observation {
	if previous := c.nodes[o.LogID]; previous != nil {
		o.Assignment = previous.Assignment
		o.RootTraceID = previous.RootTraceID
		o.RootSessionID = previous.RootSessionID
		if !o.Request.Valid {
			o.Request = previous.Request
		}
		if !o.Response.Valid {
			o.Response = previous.Response
		}
	}
	if o.RootTraceID == "" {
		o.RootTraceID = "auto-" + uuid.NewString()
		o.RootSessionID = "session-" + uuid.NewString()
	}
	if o.Request.Conversation != "" {
		o.RootSessionID = "session-" + key(domain(&o), "conversation", o.Request.Conversation)
	}
	if o.Request.NativeSession != "" {
		o.RootSessionID = "session-" + key(o.Scope, "native_session", o.Request.NativeSession)
	}
	o.Assignment = c.decide(&o)
	c.nodes[o.LogID] = &o
	changed := []Observation{o}
	// Keep missing protocol references until a parent response is observed.
	deps, _, _ := c.dependencies(&o)
	if len(deps) == 0 && o.Request.Valid {
		prefixes := prefixKeys(domain(&o), o.Request.Events)
		start := len(prefixes) - 16
		if start < 0 {
			start = 0
		}
		deps = append(deps, prefixes[start:]...)
	}
	for _, k := range deps {
		if c.waiters[k] == nil {
			c.waiters[k] = map[string]bool{}
		}
		c.waiters[k][o.LogID] = true
	}
	// Re-evaluate dependent calls when new or conflicting evidence arrives.
	queue := c.produced(&o)
	for _, k := range queue {
		c.addIndex(k, o.LogID)
	}
	changedIDs := map[string]bool{o.LogID: true}
	for steps := 0; len(queue) > 0 && steps < 8*c.maxEntries; steps++ {
		k := queue[0]
		queue = queue[1:]
		for id := range c.waiters[k] {
			n := c.nodes[id]
			if n == nil || id == o.LogID {
				continue
			}
			before := digest(n.Assignment)
			n.Assignment = c.decide(n)
			if before != digest(n.Assignment) {
				if !changedIDs[id] {
					changedIDs[id] = true
					changed = append(changed, *n)
				}
				queue = append(queue, c.produced(n)...)
			}
		}
	}
	for i := range changed {
		if n := c.nodes[changed[i].LogID]; n != nil {
			changed[i] = *n
		}
	}
	c.cleanup(o.CreatedAt)
	return changed
}

func (c *Correlator) cleanup(now time.Time) {
	for id, n := range c.nodes {
		if now.Sub(n.CreatedAt) > c.idle {
			delete(c.nodes, id)
		}
	}
	eventCount := 0
	for _, n := range c.nodes {
		eventCount += len(n.Request.Events) + len(n.Response.Events)
	}
	if len(c.nodes) > c.maxEntries || eventCount > 65536 {
		nodes := make([]*Observation, 0, len(c.nodes))
		for _, n := range c.nodes {
			nodes = append(nodes, n)
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].CreatedAt.Before(nodes[j].CreatedAt) })
		for _, n := range nodes {
			if len(c.nodes) <= c.maxEntries && eventCount <= 65536 {
				break
			}
			delete(c.nodes, n.LogID)
			eventCount -= len(n.Request.Events) + len(n.Response.Events)
		}
	}
	for _, idx := range []map[string]map[string]bool{c.index, c.waiters} {
		for k, ids := range idx {
			for id := range ids {
				if c.nodes[id] == nil {
					delete(ids, id)
				}
			}
			if len(ids) == 0 {
				delete(idx, k)
			}
		}
	}
}
