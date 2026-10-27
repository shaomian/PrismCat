package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestAutomaticTraceAndCapturePreserveForwardedTraffic(t *testing.T) {
	const first = `{"messages":[{"role":"user","content":"Fix login"}],"stream":true}`
	const second = `{"messages":[{"role":"user","content":"Fix login"},{"role":"assistant","tool_calls":[{"id":"call-1","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":"file"}]}`
	var mu sync.Mutex
	var forwarded []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		forwarded = append(forwarded, string(body))
		n := len(forwarded)
		mu.Unlock()
		if r.Header.Get("X-PrismCat-Trace-ID") != "" {
			t.Error("proxy injected a trace header")
		}
		if n == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer upstream.Close()
	cfg := &config.Config{Correlation: config.CorrelationConfig{Enabled: true}, Server: config.ServerConfig{ProxyDomains: []string{"localhost"}}, Upstreams: map[string]config.UpstreamConfig{"agent": {Target: upstream.URL, Timeout: 5}}, Logging: config.LoggingConfig{MaxRequestBody: 1 << 20, MaxResponseBody: 1 << 20, BodyPreviewBytes: 4096, StoreBase64: true}}
	r, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo := storage.NewAsyncRepository(r, cfg, 64)
	defer repo.Close()
	run, err := repo.StartCaptureRun("test", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	p := New(cfg, repo, nil, nil)
	send := func(body string) {
		request := httptest.NewRequest(http.MethodPost, "http://agent.localhost/v1/chat/completions", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		p.ServeHTTP(rr, request)
		if rr.Code != 200 {
			t.Fatalf("proxy returned %d: %s", rr.Code, rr.Body)
		}
	}
	send(first)
	send(second)
	if err := repo.StopCaptureRun(run.ID); err != nil {
		t.Fatal(err)
	}
	send(first)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		logs, total, err := repo.ListLogs(storage.LogFilter{CaptureRunID: run.ID})
		if err == nil && total == 2 && len(logs) == 2 && logs[0].TraceID != "" && logs[0].TraceID == logs[1].TraceID {
			all, count, err := repo.ListLogs(storage.LogFilter{})
			if err == nil && count == 3 && all[0].TraceID != "" && all[0].TraceID != logs[0].TraceID {
				mu.Lock()
				defer mu.Unlock()
				if len(forwarded) != 3 || forwarded[0] != first || forwarded[1] != second || forwarded[2] != first {
					t.Fatalf("forwarded bodies changed: %+v", forwarded)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	logs, _, _ := repo.ListLogs(storage.LogFilter{})
	t.Fatalf("proxy correlation/capture did not converge: %+v", logs)
}
