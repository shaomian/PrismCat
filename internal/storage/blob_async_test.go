package storage

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
)

func TestAsyncRepositoryPersistsHybridBodiesWithoutLosingPreviews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.db")
	repo, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newChunkStore(t)
	cfg := &config.Config{Logging: config.LoggingConfig{
		MaxRequestBody: 1 << 20, MaxResponseBody: 1 << 20,
		DetachBodyOverBytes: 128 << 10, BodyPreviewBytes: 64, StoreBase64: true,
	}}
	async := NewAsyncRepository(repo, cfg, 32, s)
	t.Cleanup(func() { _ = async.Close() })
	base := contextBody(512 << 10)
	response := bytes.Repeat([]byte("event: result\ndata: {\"output\":\"done\"}\n\n"), 200)
	const turns = 24
	for i := range turns {
		request := base[:(i+1)*len(base)/turns]
		if err := async.SaveLog(&RequestLog{
			ID: fmt.Sprintf("turn-%d", i), CreatedAt: time.Now(), Upstream: "agent",
			Method: "POST", Path: "/v1/responses", StatusCode: 200, Streaming: true,
			RequestHeaders:  map[string][]string{"Content-Type": {"text/plain"}},
			ResponseHeaders: map[string][]string{"Content-Type": {"text/event-stream"}},
			RequestBodyRaw:  request, ResponseBodyRaw: response,
			RequestBodySize: int64(len(request)), ResponseBodySize: int64(len(response)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := async.Close(); err != nil {
		t.Fatal(err)
	}
	if dropped := async.dropped.Load(); dropped != 0 {
		t.Fatal("logs dropped during bounded burst", dropped)
	}
	reopened, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for i := range turns {
		entry, err := reopened.GetLog(fmt.Sprintf("turn-%d", i))
		if err != nil {
			t.Fatal("accepted log not persisted", err)
		}
		request := base[:(i+1)*len(base)/turns]
		if entry.RequestBody != string(request[:64]) || entry.ResponseBody != string(response[:64]) || entry.Truncated {
			t.Fatal("preview or recoverability changed")
		}
		assertBody(t, s, entry.RequestBodyRef, request)
		assertBody(t, s, entry.ResponseBodyRef, response)
	}
	refs, err := reopened.ListBlobRefs()
	if err != nil || len(refs) == 0 {
		t.Fatal("body references missing from database", err)
	}
	ageStore(t, s)
	if _, err := s.GarbageCollect(context.Background(), refs, time.Hour); err != nil {
		t.Fatal("database references incompatible with GC", err)
	}
	for i := range turns {
		entry, err := reopened.GetLog(fmt.Sprintf("turn-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		assertBody(t, s, entry.RequestBodyRef, base[:(i+1)*len(base)/turns])
		assertBody(t, s, entry.ResponseBodyRef, response)
	}
}
