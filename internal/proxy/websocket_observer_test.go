package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/trace"
)

func testWSFrame(payload []byte, client bool, flags byte) []byte {
	b := []byte{flags}
	mask := byte(0)
	if client {
		mask = 128
	}
	if len(payload) < 126 {
		b = append(b, byte(len(payload))|mask)
	} else {
		b = append(b, 126|mask, byte(len(payload)>>8), byte(len(payload)))
	}
	key := []byte{9, 5, 3, 7}
	if client {
		b = append(b, key...)
	}
	for i, c := range payload {
		if client {
			c ^= key[i%4]
		}
		b = append(b, c)
	}
	return b
}

func TestWebSocketFIFOAndPerCallNativeMetadata(t *testing.T) {
	repo := newProxyTestRepo()
	p := New(&config.Config{Logging: config.LoggingConfig{MaxRequestBody: 4096, MaxResponseBody: 4096}}, repo, nil, nil)
	o := p.newResponsesObserver(&storage.RequestLog{ID: "connection", RequestHeaders: map[string][]string{"X-Codex-Turn-Metadata": {`{"thread_id":"old","turn_id":"stale"}`}}}, nil)
	for _, request := range []string{`{"type":"response.create","input":"first"}`, `{"type":"response.create","input":"second","client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"new\",\"turn_id\":\"current\"}"}}`} {
		o.enqueue(true, testWSFrame([]byte(request), true, 129))
	}
	for _, response := range []string{`{"type":"response.completed","response":{"id":"one","output":[]}}`, `{"type":"response.completed","response":{"id":"two","output":[]}}`} {
		o.enqueue(false, testWSFrame([]byte(response), false, 129))
	}
	if reason := o.close(); reason != "" {
		t.Fatal(reason)
	}
	if len(repo.logs) != 4 {
		t.Fatalf("wrong number of snapshots: %d", len(repo.logs))
	}
	first, second := repo.logs[2], repo.logs[3]
	if !strings.Contains(string(first.RequestBodyRaw), "first") || !strings.Contains(string(first.ResponseBodyRaw), `"one"`) || !strings.Contains(string(second.ResponseBodyRaw), `"two"`) {
		t.Fatal("FIFO responses assigned to wrong request")
	}
	old := trace.ParseClientRequest("/v1/responses", first.RequestBodyRaw, first.RequestHeaders)
	current := trace.ParseClientRequest("/v1/responses", second.RequestBodyRaw, second.RequestHeaders)
	if old.NativeTurn != "" || current.NativeTurn == "" {
		t.Fatalf("handshake metadata leaked or call metadata ignored: %+v %+v", old, current)
	}
}

func TestWebSocketObserverIncompleteAndUnscopedErrors(t *testing.T) {
	for _, payload := range []string{"", `{"type":"error","status":400,"error":{"message":"unknown lane"}}`, strings.Repeat("x", 1024)} {
		repo := newProxyTestRepo()
		p := New(&config.Config{Logging: config.LoggingConfig{MaxRequestBody: 4096, MaxResponseBody: 256}}, repo, nil, nil)
		o := p.newResponsesObserver(&storage.RequestLog{ID: "connection"}, nil)
		o.enqueue(true, testWSFrame([]byte(`{"type":"response.create","input":"task"}`), true, 129))
		if payload != "" {
			o.enqueue(false, testWSFrame([]byte(payload), false, 129))
		}
		if reason := o.close(); reason == "" {
			t.Fatal("partial call reported complete")
		}
		last := repo.logs[len(repo.logs)-1]
		if last.StatusCode != 0 || !last.ResponseBodyCaptureTruncated || last.WebSocket.ObservationError == "" {
			t.Fatalf("partial call became success: %+v", last)
		}
		if strings.Contains(payload, "unknown lane") && !strings.Contains(string(o.unmatched), "unknown lane") {
			t.Fatal("unowned error payload lost")
		}
	}
}

func TestWebSocketObserverReportsFailureBeforeConnectionCloses(t *testing.T) {
	repo := newProxyTestRepo()
	p := New(&config.Config{Logging: config.LoggingConfig{MaxRequestBody: 4096, MaxResponseBody: 4096}}, repo, nil, nil)
	reported := make(chan string, 1)
	o := p.newResponsesObserver(&storage.RequestLog{ID: "connection"}, nil, func(reason string, _ []byte) { reported <- reason })
	defer o.close()
	o.enqueue(true, testWSFrame([]byte(`{"type":"response.create","input":"task"}`), true, 129))
	o.enqueue(false, testWSFrame([]byte(`{"type":"error","status":400}`), false, 129))
	select {
	case reason := <-reported:
		if reason != "unscoped_websocket_error" || len(repo.logs) != 2 || !repo.logs[1].ResponseBodyCaptureTruncated {
			t.Fatal("failure not persisted before callback")
		}
	case <-time.After(time.Second):
		t.Fatal("failure waited for connection close")
	}
}

func readTestWSFrame(r io.Reader) ([]byte, []byte, error) {
	b := make([]byte, 2)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, nil, err
	}
	n := int(b[1] & 127)
	if n == 126 {
		extra := make([]byte, 2)
		if _, err := io.ReadFull(r, extra); err != nil {
			return nil, nil, err
		}
		b = append(b, extra...)
		n = int(binary.BigEndian.Uint16(extra))
	}
	masked := b[1]&128 != 0
	var key []byte
	if masked {
		key = make([]byte, 4)
		if _, err := io.ReadFull(r, key); err != nil {
			return nil, nil, err
		}
		b = append(b, key...)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	b = append(b, payload...)
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return b, payload, nil
}

func TestWSFramesFragmentationMaskControlAndCompression(t *testing.T) {
	for _, client := range []bool{false, true} {
		f := wsFrames{client: client}
		raw := append(testWSFrame([]byte("hel"), client, 1), testWSFrame([]byte("ping"), client, 137)...)
		raw = append(raw, testWSFrame([]byte("lo"), client, 128)...)
		var messages []string
		for _, b := range raw {
			if err := f.feed([]byte{b}, func(message []byte) error { messages = append(messages, string(message)); return nil }); err != nil {
				t.Fatal(err)
			}
		}
		if len(messages) != 1 || messages[0] != "hello" {
			t.Fatalf("bad reassembly: %v", messages)
		}
	}
	for _, takeover := range []bool{false, true} {
		f := wsFrames{}
		extension := "permessage-deflate"
		if !takeover {
			extension += "; server_no_context_takeover"
		}
		if err := f.configure(extension); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		writer, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		for range 2 {
			if !takeover {
				writer.Reset(&buf)
			}
			original := strings.Repeat("compressed response content ", 200)
			_, _ = writer.Write([]byte(original))
			_ = writer.Flush()
			compressed := append([]byte{}, buf.Bytes()...)
			buf.Reset()
			compressed = compressed[:len(compressed)-4]
			if err := f.feed(testWSFrame(compressed, false, 193), func(message []byte) error {
				if string(message) != original {
					t.Fatal("decompression changed content")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		_ = writer.Close()
	}
	if err := (&wsFrames{}).feed([]byte{129, 127, 0, 0, 0, 1, 0, 0, 0, 0}, func([]byte) error { return nil }); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestResponsesWebSocketCallsCorrelateAcrossReusedAndParallelLanes(t *testing.T) {
	requests := []string{
		`{"type":"response.create","stream_id":"a","input":"Fix login"}`,
		`{"type":"response.create","stream_id":"b","input":"Write documentation"}`,
		`{"type":"response.create","stream_id":"a","previous_response_id":"ra","input":[{"type":"function_call_output","call_id":"tc","output":"file"}]}`,
		`{"type":"response.create","stream_id":"a","previous_response_id":"ra2","input":"Now add export"}`,
	}
	responses := [][]string{
		{`{"type":"response.created","stream_id":"b","response":{"id":"rb"}}`, `{"type":"response.created","stream_id":"a","response":{"id":"ra"}}`, `{"type":"response.completed","stream_id":"b","response":{"id":"rb","output":[]}}`, `{"type":"response.completed","stream_id":"a","response":{"id":"ra","output":[{"type":"function_call","call_id":"tc","name":"read","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`},
		{`{"type":"response.completed","stream_id":"a","response":{"id":"ra2","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}}`},
		{`{"type":"response.completed","stream_id":"a","response":{"id":"ra3","output":[]}}`},
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: test\r\n\r\n")
		_ = rw.Flush()
		for i, request := range requests {
			raw, payload, err := readTestWSFrame(rw)
			if err != nil {
				t.Error(err)
				return
			}
			if string(payload) != request || !bytes.Equal(raw, testWSFrame([]byte(request), true, 129)) {
				t.Error("proxy changed upstream bytes")
				return
			}
			if i == 0 {
				continue
			}
			for _, response := range responses[i-1] {
				if _, err := conn.Write(testWSFrame([]byte(response), false, 129)); err != nil {
					t.Error(err)
					return
				}
			}
		}
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer upstream.Close()
	cfg := &config.Config{Correlation: config.CorrelationConfig{Enabled: true}, Server: config.ServerConfig{ProxyDomains: []string{"localhost"}}, Upstreams: map[string]config.UpstreamConfig{"agent": {Target: upstream.URL, Timeout: 5}}, Logging: config.LoggingConfig{MaxRequestBody: 1 << 20, MaxResponseBody: 1 << 20, BodyPreviewBytes: 8192, StoreBase64: true}}
	cfg.Usage = config.UsageExtractionConfig{Enabled: true, Upstreams: map[string]config.UsageExtractionUpstreamBinding{"agent": {Enabled: true, RuleNames: []string{"Responses"}}}, Rules: []config.UsageExtractionRule{{Name: "Responses", Enabled: true, Match: config.UsageExtractionMatch{ContentTypes: []string{"text/event-stream"}}, Paths: config.UsageExtractionPaths{InputTokens: []string{"/response/usage/input_tokens"}, OutputTokens: []string{"/response/usage/output_tokens"}, TotalTokens: []string{"/response/usage/total_tokens"}}}}}
	store, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo := storage.NewAsyncRepository(store, cfg, 128)
	defer repo.Close()
	firstRun, err := repo.StartCaptureRun("first", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(cfg, repo, nil, nil))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = fmt.Fprint(conn, "GET /v1/responses HTTP/1.1\r\nHost: agent.localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: key\r\nSec-WebSocket-Version: 13\r\n\r\n")
	reader := bufio.NewReader(conn)
	handshake, err := http.ReadResponse(reader, nil)
	if err != nil || handshake.StatusCode != 101 {
		t.Fatalf("bad handshake: %v %v", handshake, err)
	}
	write := func(i int) {
		if _, err := conn.Write(testWSFrame([]byte(requests[i]), true, 129)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(batch int) {
		for _, response := range responses[batch] {
			raw, _, err := readTestWSFrame(reader)
			if err != nil || !bytes.Equal(raw, testWSFrame([]byte(response), false, 129)) {
				t.Fatalf("downstream bytes changed: %s %v", raw, err)
			}
		}
	}
	write(0)
	write(1)
	read(0)
	if err := repo.StopCaptureRun(firstRun.ID); err != nil {
		t.Fatal(err)
	}
	secondRun, err := repo.StartCaptureRun("second", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	write(2)
	read(1)
	write(3)
	read(2)
	_ = conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		logs, total, err := repo.ListLogs(storage.LogFilter{Method: "POST"})
		if err == nil && total == 4 {
			found := map[string]*storage.RequestLog{}
			for _, entry := range logs {
				full, _ := repo.GetLog(entry.ID)
				if full != nil && full.TraceID != "" && full.StatusCode == 200 {
					found[full.RequestBody] = full
				}
			}
			if len(found) == 4 {
				a, b, continuation, next := found[requests[0]], found[requests[1]], found[requests[2]], found[requests[3]]
				if a == nil || b == nil || continuation == nil || next == nil {
					t.Fatalf("request bodies changed: %+v", found)
				}
				if a.TraceID != continuation.TraceID || a.TraceID == b.TraceID || a.TraceID == next.TraceID || a.TraceAssociation.SessionID != next.TraceAssociation.SessionID {
					t.Fatalf("incorrect grouping: %+v %+v %+v %+v", a.TraceAssociation, b.TraceAssociation, continuation.TraceAssociation, next.TraceAssociation)
				}
				if a.CaptureRunID != firstRun.ID || b.CaptureRunID != firstRun.ID || continuation.CaptureRunID != secondRun.ID || next.CaptureRunID != secondRun.ID {
					t.Fatal("capture membership followed connection instead of response.create")
				}
				if a.WebSocket == nil || a.WebSocket.ConnectionID != next.WebSocket.ConnectionID || a.WebSocket.StreamID != "a" || b.WebSocket.StreamID != "b" {
					t.Fatal("transport metadata missing")
				}
				if a.UsageInputTokens == nil || *a.UsageInputTokens != 10 {
					t.Fatalf("WebSocket usage lost: %+v", a)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("WebSocket call logs/correlation did not converge")
}
