package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/trace"
)

type wsChunk struct {
	client  bool
	raw     []byte
	at      time.Time
	capture string
}

type wsCall struct {
	log        *storage.RequestLog
	responseID string
	body       []byte
}

type responsesObserver struct {
	proxy                       *Proxy
	base                        *storage.RequestLog
	queue                       chan wsChunk
	done                        chan struct{}
	queued                      atomic.Int64
	failed                      atomic.Bool
	reason                      string // read after done is closed
	unmatched                   []byte // unowned protocol event, retained on the handshake log
	onFailure                   func(string, []byte)
	lanes                       map[string][]*wsCall
	bytes                       int
	requestLimit, responseLimit int64
	frames                      [2]wsFrames
}

func (p *Proxy) newResponsesObserver(base *storage.RequestLog, headers http.Header, reports ...func(string, []byte)) *responsesObserver {
	cfg := p.cfg.LoggingSnapshot()
	o := &responsesObserver{proxy: p, base: base.Clone(), queue: make(chan wsChunk, 128), done: make(chan struct{}), lanes: map[string][]*wsCall{}, requestLimit: cfg.MaxRequestBody, responseLimit: cfg.MaxResponseBody}
	if len(reports) > 0 {
		o.onFailure = reports[0]
	}
	if o.requestLimit > trace.MaxParseBytes {
		o.requestLimit = trace.MaxParseBytes
	}
	if o.responseLimit > trace.MaxParseBytes {
		o.responseLimit = trace.MaxParseBytes
	}
	o.frames[0].client = true
	for i := range o.frames {
		if err := o.frames[i].configure(headers.Get("Sec-WebSocket-Extensions")); err != nil {
			o.reason = err.Error()
			o.failed.Store(true)
		}
	}
	go o.run()
	return o
}

type observedWSReader struct {
	io.Reader
	observer *responsesObserver
	client   bool
}

func (r observedWSReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if n > 0 {
		r.observer.enqueue(r.client, b[:n])
	}
	return n, err
}

func (o *responsesObserver) enqueue(client bool, raw []byte) {
	if o.failed.Load() {
		return
	}
	n := int64(len(raw))
	if o.queued.Add(n) > 2<<20 {
		o.queued.Add(-n)
		o.failed.Store(true)
		return
	}
	chunk := wsChunk{client: client, raw: append([]byte{}, raw...), at: time.Now()}
	if client {
		if registry, ok := o.proxy.repo.(interface{ CaptureRunForUpstream(string) string }); ok {
			chunk.capture = registry.CaptureRunForUpstream(o.base.Upstream)
		}
	}
	select {
	case o.queue <- chunk:
	default:
		o.queued.Add(-n)
		o.failed.Store(true)
	}
}

func (o *responsesObserver) close() string {
	close(o.queue)
	<-o.done
	return o.reason
}

func (o *responsesObserver) run() {
	defer close(o.done)
	reported := false
	report := func() {
		if reported {
			return
		}
		reported = true
		if o.reason == "" {
			o.reason = "websocket_observer_queue_limit"
		}
		for _, calls := range o.lanes {
			for _, call := range calls {
				o.finish(call, 0, o.reason, true)
			}
		}
		o.lanes, o.bytes = map[string][]*wsCall{}, 0
		if o.onFailure != nil {
			o.onFailure(o.reason, o.unmatched)
		}
	}
	if o.failed.Load() {
		report()
	}
	for chunk := range o.queue {
		o.queued.Add(-int64(len(chunk.raw)))
		if o.failed.Load() {
			report()
			continue
		}
		i := 1
		if chunk.client {
			i = 0
		}
		if err := o.frames[i].feed(chunk.raw, func(raw []byte) error { return o.event(chunk, raw) }); err != nil {
			o.reason = err.Error()
			o.failed.Store(true)
		}
		if o.failed.Load() {
			report()
		}
	}
	if o.failed.Load() && o.reason == "" {
		o.reason = "websocket_observer_queue_limit"
	}
	if o.reason == "" && (o.frames[0].opcode != 0 || o.frames[1].opcode != 0 || len(o.frames[0].buffer) > 0 || len(o.frames[1].buffer) > 0) {
		o.reason = "incomplete_websocket_frame"
	}
	if o.reason != "" || len(o.lanes) > 0 {
		if o.reason == "" {
			o.reason = "websocket_closed_before_response_completed"
		}
		report()
	}
}

func (o *responsesObserver) event(chunk wsChunk, raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid_websocket_json")
	}
	var event struct {
		Type       string `json:"type"`
		StreamID   string `json:"stream_id"`
		ResponseID string `json:"response_id"`
		Response   struct {
			ID string `json:"id"`
		} `json:"response"`
		Status int `json:"status"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return fmt.Errorf("invalid_websocket_json")
	}
	if chunk.client {
		if event.Type != "response.create" {
			return fmt.Errorf("unsupported_websocket_client_event")
		}
		count := 0
		for _, calls := range o.lanes {
			count += len(calls)
		}
		if count >= 64 || o.bytes+len(raw) > 16<<20 {
			return fmt.Errorf("websocket_pending_call_limit")
		}
		entry := o.base.Clone()
		entry.ID, entry.Method, entry.CreatedAt = uuid.NewString(), http.MethodPost, chunk.at
		entry.TraceSeq = 0
		if entry.TraceID != "" && o.proxy.traceSeq != nil {
			entry.TraceSeq = o.proxy.traceSeq.Next(entry.TraceID)
		}
		entry.CaptureRunID = chunk.capture
		entry.WebSocket = &storage.WebSocketInfo{ConnectionID: o.base.ID, StreamID: event.StreamID, Kind: "response"}
		entry.RequestHeadersOriginal = nil
		if len(o.base.RequestHeadersOriginal) > 0 {
			entry.RequestHeaders = storage.CloneHeaders(o.base.RequestHeadersOriginal)
		}
		if entry.RequestHeaders == nil {
			entry.RequestHeaders = map[string][]string{}
		}
		for name := range entry.RequestHeaders {
			// Handshake turn metadata may be stale after connection reuse.
			if strings.EqualFold(name, "x-codex-turn-metadata") || strings.EqualFold(name, "Content-Encoding") || strings.EqualFold(name, "Content-Length") {
				delete(entry.RequestHeaders, name)
			}
		}
		entry.RequestHeaders["Content-Type"] = []string{"application/json"}
		entry.ResponseHeaders = map[string][]string{"Content-Type": {"text/event-stream"}}
		entry.Streaming = true
		entry.RequestBodyRaw, entry.RequestBodySize = append([]byte{}, raw...), int64(len(raw))
		if int64(len(raw)) > o.requestLimit {
			entry.RequestBodyRaw = nil
			entry.RequestBodyCaptureTruncated = true
		}
		call := &wsCall{log: entry}
		o.lanes[event.StreamID] = append(o.lanes[event.StreamID], call)
		o.bytes += len(entry.RequestBodyRaw)
		o.proxy.saveLogSnapshot(entry)
		o.proxy.publishInitialLive(entry)
		return nil
	}
	if event.Type == "error" && event.StreamID == "" {
		// Unscoped errors can refer to the connection or an invalid named lane.
		// Preserve pending calls as incomplete instead of guessing an owner.
		return o.unowned(raw, "unscoped_websocket_error")
	}
	calls := o.lanes[event.StreamID]
	if len(calls) == 0 {
		return o.unowned(raw, "unmatched_websocket_response")
	}
	call := calls[0]
	id := event.ResponseID
	if event.Response.ID != "" {
		id = event.Response.ID
	}
	if id != "" {
		if call.responseID != "" && call.responseID != id {
			return o.unowned(raw, "conflicting_websocket_response")
		}
		call.responseID = id
	}
	// Store decoded application events in SSE form so existing inspectors,
	// usage extraction and timeline parsing work for both transports.
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return fmt.Errorf("invalid_websocket_json")
	}
	part := []byte("data: " + compact.String() + "\n\n")
	if int64(len(call.body)+len(part)) > o.responseLimit || o.bytes+len(part) > 16<<20 {
		return fmt.Errorf("websocket_response_capture_limit")
	}
	call.body = append(call.body, part...)
	o.bytes += len(part)
	if call.log.StatusCode == 0 {
		call.log.StatusCode = http.StatusOK
		o.proxy.publishHeaders(call.log)
	}
	switch event.Type {
	case "response.completed", "response.failed", "response.incomplete", "error":
		status, reason := http.StatusOK, ""
		if event.Type != "response.completed" {
			status, reason = http.StatusBadGateway, event.Type
			if event.Status >= 400 && event.Status <= 599 {
				status = event.Status
			}
		}
		o.finish(call, status, reason, false)
		o.bytes -= len(call.log.RequestBodyRaw) + len(call.body)
		if len(calls) == 1 {
			delete(o.lanes, event.StreamID)
		} else {
			o.lanes[event.StreamID] = calls[1:]
		}
	}
	return nil
}

func (o *responsesObserver) unowned(raw []byte, reason string) error {
	if int64(len(raw)) <= o.responseLimit {
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil {
			o.unmatched = []byte("data: " + compact.String() + "\n\n")
		}
	}
	return fmt.Errorf("%s", reason)
}

func (o *responsesObserver) finish(call *wsCall, status int, reason string, truncated bool) {
	entry := call.log.Clone()
	entry.StatusCode, entry.Error = status, reason
	if truncated {
		entry.Error = ""
		entry.WebSocket.ObservationError = reason
	}
	entry.ResponseBodyRaw = call.body
	entry.ResponseBodySize = int64(len(call.body))
	entry.ResponseBodyCaptureTruncated = truncated
	entry.CorrelationFinal = true
	entry.Latency = time.Since(entry.CreatedAt).Milliseconds()
	o.proxy.saveLogSnapshot(entry)
	o.proxy.completeLive(entry)
}
