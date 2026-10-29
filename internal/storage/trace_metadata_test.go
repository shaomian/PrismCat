package storage

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTraceMetadataPreservesFieldsWithoutBodies(t *testing.T) {
	repo := mustNewSQLiteRepoForTest(t)
	defer repo.Close()
	log := newTestLog("log", time.Now().UTC())
	log.TraceID, log.TraceSeq = "trace", 1
	log.RequestBody, log.RequestBodyOriginal, log.RequestBodyFinal, log.ResponseBody = "request", "original", "final", "response"
	log.RequestBodyRef, log.ResponseBodyRef = "request-ref", "response-ref"
	log.RequestBodySize, log.ResponseBodySize = 7000, 8000
	log.RequestHeaders, log.ResponseHeaders = map[string][]string{"Content-Type": {"application/json"}}, map[string][]string{"Content-Type": {"text/event-stream"}}
	log.StatusCode, log.Latency, log.Streaming = 200, 150, true
	if err := repo.SaveLog(log); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveLogAnnotation(log.ID, LogAnnotation{Saved: true, Note: "keep", Labels: []string{"test"}}); err != nil {
		t.Fatal(err)
	}
	full, err := repo.GetTraceRequests("trace")
	if err != nil {
		t.Fatal(err)
	}
	async := &AsyncRepository{inner: repo}
	metadata, err := async.GetTraceRequestMetadata("trace")
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || len(metadata) != 1 || full[0].RequestBody == "" || full[0].ResponseBody == "" {
		t.Fatal("expected complete source logs")
	}
	full[0].RequestBody, full[0].RequestBodyOriginal, full[0].RequestBodyFinal, full[0].ResponseBody = "", "", "", ""
	if !reflect.DeepEqual(full, metadata) {
		t.Fatalf("metadata differs beyond body fields: full=%+v metadata=%+v", full[0], metadata[0])
	}
}

func BenchmarkTraceRequestMetadata(b *testing.B) {
	repo, err := NewSQLiteRepository(filepath.Join(b.TempDir(), "trace.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repo.Close() })
	for i := range 100 {
		log := newTestLog(fmt.Sprint(i), time.Now().UTC())
		log.TraceID, log.TraceSeq = "trace", i+1
		log.RequestBody, log.ResponseBody = strings.Repeat("x", 64*1024), strings.Repeat("y", 64*1024)
		if err := repo.SaveLog(log); err != nil {
			b.Fatal(err)
		}
	}
	for _, metadata := range []bool{false, true} {
		b.Run(fmt.Sprintf("metadata=%t", metadata), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := repo.getTraceRequests("trace", metadata); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
