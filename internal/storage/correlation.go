package storage

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/shaomian/PrismCat/internal/httpbody"
	"github.com/shaomian/PrismCat/internal/trace"
)

func correlationSize(entry *RequestLog) int64 {
	return int64(len(entry.RequestBodyRaw) + len(entry.RequestBodyOriginalRaw) + len(entry.ResponseBodyRaw))
}

func (a *AsyncRepository) enqueueCorrelation(entry *RequestLog) {
	if a.cfg == nil || a.correlator == nil || !a.cfg.CorrelationSnapshot().Enabled {
		return
	}
	// A separate byte-bounded queue keeps parsing off the SQLite log writer.
	if len(entry.RequestBodyRaw) > trace.MaxParseBytes {
		entry.RequestBodyRaw = nil
		entry.RequestBodyCaptureTruncated = true
	}
	if len(entry.RequestBodyOriginalRaw) > trace.MaxParseBytes {
		entry.RequestBodyOriginalRaw = nil
		entry.RequestBodyCaptureTruncated = true
	}
	if len(entry.ResponseBodyRaw) > trace.MaxParseBytes {
		entry.ResponseBodyRaw = nil
		entry.ResponseBodyCaptureTruncated = true
	}
	entry.RequestBodyFinalRaw = nil
	n := correlationSize(entry)
	if a.correlationBytes.Add(n) > 16<<20 {
		a.correlationBytes.Add(-n)
		a.correlationDropped.Add(1)
		return
	}
	select {
	case a.correlationCh <- entry:
	default:
		a.correlationBytes.Add(-n)
		a.correlationDropped.Add(1)
	}
}

func decodeTraceInfo(entry *RequestLog, raw string) {
	if raw != "" {
		var a trace.Assignment
		if json.Unmarshal([]byte(raw), &a) == nil {
			entry.TraceAssociation = &a
		}
	}
	if entry.TraceAssociation == nil && entry.TraceID != "" {
		entry.TraceAssociation = &trace.Assignment{TraceID: entry.TraceID, Source: "explicit", Reason: "explicit_header", State: "declared"}
	}
}

func featureBody(raw []byte, headers map[string][]string, truncated bool) []byte {
	if truncated || len(raw) > trace.MaxParseBytes {
		return nil
	}
	ct := FirstHeaderValue(headers, "Content-Type")
	if !strings.Contains(ct, "json") && !strings.Contains(ct, "event-stream") {
		return nil
	}
	formatted := httpbody.FormatPreviewForDisplay(ct, FirstHeaderValue(headers, "Content-Encoding"), raw, httpbody.FormatOptions{MaxOutputBytes: trace.MaxParseBytes, RequireContentEncodingDecode: true})
	if formatted.Truncated || formatted.DecodeFailed || formatted.Binary {
		return nil
	}
	return []byte(formatted.Text)
}

func (a *AsyncRepository) correlationObservation(entry *RequestLog) *trace.Observation {
	if a.cfg == nil || a.correlator == nil || !a.cfg.CorrelationSnapshot().Enabled {
		return nil
	}
	// Original client context is authoritative when request overrides are enabled.
	req := entry.RequestBodyRaw
	if len(entry.RequestBodyOriginalRaw) > 0 {
		req = entry.RequestBodyOriginalRaw
	}
	headers := entry.RequestHeaders
	if len(entry.RequestHeadersOriginal) > 0 {
		headers = entry.RequestHeadersOriginal
	}
	f := trace.ParseClientRequest(entry.Path, featureBody(req, headers, entry.RequestBodyCaptureTruncated), headers)
	if !f.Valid && (!trace.SupportedPath(entry.Path) || entry.Method != "POST" || (len(req) == 0 && !entry.CorrelationFinal)) {
		return nil
	}
	o := &trace.Observation{LogID: entry.ID, Scope: entry.Upstream, CreatedAt: entry.CreatedAt, Request: f}
	if entry.CorrelationFinal && !entry.ResponseBodyCaptureTruncated && entry.StatusCode >= 200 && entry.StatusCode < 300 {
		o.Response = trace.ParseResponse(f.Format, featureBody(entry.ResponseBodyRaw, entry.ResponseHeaders, false), entry.Streaming)
	}
	if entry.TraceID != "" {
		o.Assignment = trace.Assignment{TraceID: entry.TraceID, SessionID: "explicit-" + entry.TraceID, Source: "explicit", Reason: "explicit_header", State: "declared"}
	}
	return o
}

func (a *AsyncRepository) processCorrelation(o *trace.Observation, entry *RequestLog) {
	if o == nil {
		return
	}
	store, ok := a.inner.(*SQLiteRepository)
	if !ok {
		return
	}
	a.correlationMu.Lock()
	defer a.correlationMu.Unlock()
	// 配置热更新跟随:每条观测处理前对齐一次内存边界,成本是一次快照读
	idleWindow := trace.DefaultIdle
	if a.cfg != nil {
		snapshot := a.cfg.CorrelationSnapshot()
		a.correlator.Tune(snapshot.MaxEntries, snapshot.IdleWindow())
		idleWindow = snapshot.IdleWindow()
	}
	// A final snapshot may have been queued before the user removed its trace.
	var deleted bool
	if err := store.db.QueryRow("SELECT EXISTS(SELECT 1 FROM deleted_log_ids WHERE id=?)", entry.ID).Scan(&deleted); err != nil || deleted {
		return
	}
	if entry.CaptureRunID != "" && o.Assignment.Source != "explicit" {
		if assignment, ok := store.captureAssignment(entry.CaptureRunID); ok {
			o.Assignment = assignment
		}
	}
	for _, changed := range a.correlator.Observe(*o) {
		assignment, err := store.storeObservation(changed, false)
		if err != nil {
			log.Printf("trace correlation persistence failed: %v", err)
			continue
		}
		changed.Assignment = assignment
		a.correlator.Restore(changed)
	}
	// Feature summaries are recoverable only within the active idle window.
	if time.Since(a.correlationPruned) > time.Minute {
		if _, err := store.db.Exec("UPDATE trace_assignments SET features='' WHERE features!='' AND updated_at<?", time.Now().Add(-idleWindow).UnixMilli()); err != nil {
			log.Printf("trace feature cleanup failed: %v", err)
		}
		a.correlationPruned = time.Now()
	}
}

func (a *AsyncRepository) CorrelationStatus() map[string]any {
	return map[string]any{"queued_bytes": a.correlationBytes.Load(), "dropped_observations": a.correlationDropped.Load()}
}

func (a *AsyncRepository) GetTraceMetadata(id string) (*RequestLog, error) {
	if r, ok := a.inner.(interface {
		GetTraceMetadata(string) (*RequestLog, error)
	}); ok {
		return r.GetTraceMetadata(id)
	}
	return a.inner.GetLog(id)
}

func (a *AsyncRepository) ListCaptureRuns() ([]CaptureRun, error) {
	if r, ok := a.inner.(CorrelationRepository); ok {
		return r.ListCaptureRuns()
	}
	return nil, errors.New("capture unavailable")
}

func (a *AsyncRepository) ActiveCaptureRuns() []CaptureRun {
	if r, ok := a.inner.(interface{ ActiveCaptureRuns() []CaptureRun }); ok {
		return r.ActiveCaptureRuns()
	}
	return nil
}

func (a *AsyncRepository) ListCaptureRunsPage(limit, offset int) ([]CaptureRun, int, error) {
	if r, ok := a.inner.(interface {
		ListCaptureRunsPage(int, int) ([]CaptureRun, int, error)
	}); ok {
		return r.ListCaptureRunsPage(limit, offset)
	}
	runs, err := a.ListCaptureRuns()
	total := len(runs)
	offset = min(max(offset, 0), total)
	return runs[offset:min(offset+max(limit, 1), total)], total, err
}
func (a *AsyncRepository) StartCaptureRun(name, upstream string, single bool) (CaptureRun, error) {
	if r, ok := a.inner.(CorrelationRepository); ok {
		return r.StartCaptureRun(name, upstream, single)
	}
	return CaptureRun{}, errors.New("capture unavailable")
}
func (a *AsyncRepository) StopCaptureRun(id string) error {
	if r, ok := a.inner.(CorrelationRepository); ok {
		return r.StopCaptureRun(id)
	}
	return errors.New("capture unavailable")
}
func (a *AsyncRepository) CaptureRunForUpstream(upstream string) string {
	if r, ok := a.inner.(CorrelationRepository); ok {
		return r.CaptureRunForUpstream(upstream)
	}
	return ""
}
func (a *AsyncRepository) AssignTrace(id, target string, revision int64) (trace.Assignment, error) {
	a.correlationMu.Lock()
	defer a.correlationMu.Unlock()
	repo, ok := a.inner.(CorrelationRepository)
	if !ok {
		return trace.Assignment{}, errors.New("trace correction unavailable")
	}
	assignment, err := repo.AssignTrace(id, target, revision)
	if err == nil && a.correlator != nil {
		if observations, loadErr := a.inner.(*SQLiteRepository).loadObservations(); loadErr == nil {
			for _, o := range observations {
				a.correlator.Restore(o)
			}
		}
	}
	return assignment, err
}
