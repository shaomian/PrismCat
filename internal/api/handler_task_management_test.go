package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/timeline"
)

func TestTaskManagementRoutesPreviewAtomicApplyAndArchiveFilters(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	async := storage.NewAsyncRepository(repo, &config.Config{}, 16)
	defer async.Close()
	handler := New(&config.Config{}, async, nil, nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	request := func(method, path string, value any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(value)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(string(body))))
		return rr
	}
	for _, id := range []string{"one", "two"} {
		if err = repo.SaveLog(&storage.RequestLog{ID: id, TraceID: id, StatusCode: 200, CreatedAt: time.Now(), Upstream: "agent", Path: "/v1/responses", Method: "POST"}); err != nil {
			t.Fatal(err)
		}
	}
	payload := storage.TaskManagementRequest{Kind: "trace", Action: "delete", Items: []storage.TaskManagementItem{{ID: "one"}, {ID: "missing"}}}
	rr := request("POST", "/api/task-management/preview", payload)
	var preview storage.TaskManagementPreview
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &preview) != nil || preview.CanApply || preview.Items[1].BlockedReason != "task_not_found" {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body)
	}
	rr = request("POST", "/api/task-management/apply", payload)
	if rr.Code != 409 || !strings.Contains(rr.Body.String(), "task_management_blocked") {
		t.Fatalf("blocked apply: %d %s", rr.Code, rr.Body)
	}
	if _, err = repo.GetLog("one"); err != nil {
		t.Fatal("partial apply deleted valid trace")
	}
	payload.Action = "archive"
	payload.Items = []storage.TaskManagementItem{{ID: "one"}}
	if rr = request("POST", "/api/task-management/apply", payload); rr.Code != 200 {
		t.Fatalf("archive: %d %s", rr.Code, rr.Body)
	}
	if rr = request("GET", "/api/traces?archived=true", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"trace_id":"one"`) {
		t.Fatalf("archived trace filter: %s", rr.Body)
	}
	payload.Action = "restore"
	if rr = request("POST", "/api/task-management/apply", payload); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	handler.tlCache.put("one", storage.TraceFingerprint{}, &timeline.Timeline{})
	handler.tlCache.put("two", storage.TraceFingerprint{}, &timeline.Timeline{})
	payload.Action = "delete"
	rr = request("POST", "/api/task-management/preview", payload)
	if json.Unmarshal(rr.Body.Bytes(), &preview) != nil || !preview.CanApply {
		t.Fatal(rr.Body)
	}
	revision := preview.Items[0].Revision
	payload.Items[0].Revision = &revision
	if rr = request("POST", "/api/task-management/apply", payload); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"deleted_logs":1`) {
		t.Fatalf("delete: %d %s", rr.Code, rr.Body)
	}
	if handler.tlCache.entries["one"] != nil || handler.tlCache.entries["two"] == nil {
		t.Fatal("deleted timeline retained, or unrelated cache dropped")
	}
	capture, err := repo.StartCaptureRun("recording", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	payload = storage.TaskManagementRequest{Kind: "capture", Action: "archive", Items: []storage.TaskManagementItem{{ID: capture.ID}}}
	if rr = request("POST", "/api/task-management/apply", payload); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr = request("GET", "/api/capture-runs?archived=true&limit=20", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"archived":true`) {
		t.Fatalf("capture archive filter: %s", rr.Body)
	}
	if rr = request("GET", "/api/capture-runs?archived=false&limit=20", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"total":0`) {
		t.Fatalf("normal capture filter: %s", rr.Body)
	}
	template, err := repo.SaveReviewTemplate(benchmark.ReviewTemplate{Name: "custom", Config: benchmark.ReviewConfig{Points: []benchmark.ReviewPoint{{ID: "tokens", Name: "Token"}}, Selected: []string{"tokens"}}})
	if err != nil {
		t.Fatal(err)
	}
	payload = storage.TaskManagementRequest{Kind: "template", Action: "archive", Items: []storage.TaskManagementItem{{ID: template.ID}}}
	if rr = request("POST", "/api/task-management/apply", payload); rr.Code != 200 {
		t.Fatal(rr.Body)
	}
	if rr = request("GET", "/api/review-templates?archived=true", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), template.ID) {
		t.Fatalf("template archive filter: %s", rr.Body)
	}
	if rr = request("GET", "/api/review-templates?archived=false", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"total":0`) {
		t.Fatalf("normal template filter: %s", rr.Body)
	}
}

func TestTaskManagementRejectsInvalidRequestsAndReviewPreparationRace(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	handler := New(&config.Config{}, repo, nil, nil)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handler.handleTaskManagement(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	for _, body := range []string{`{}`, `{"kind":"trace","action":"delete","items":[]}`, `{"kind":"trace","action":"delete","items":[{"id":"a"},{"id":"a"}]}`, `{"kind":"run","action":"delete","items":[{"id":"a"}]}`, `{"kind":"trace","action":"delete","items":[{"id":"a"}],"unexpected":true}`} {
		if rr := request("POST", "/api/task-management/apply", body); rr.Code != 400 {
			t.Fatalf("invalid request accepted: %s %d", body, rr.Code)
		}
	}
	if rr := request("GET", "/api/task-management/apply", ""); rr.Code != 405 {
		t.Fatal("non POST mutation allowed")
	}
	card, err := repo.SaveExperiment(benchmark.Experiment{Name: "preparing review"})
	if err != nil {
		t.Fatal(err)
	}
	handler.reviewActive[card.ID] = true
	payload, _ := json.Marshal(storage.TaskManagementRequest{Kind: "experiment", Action: "delete", Items: []storage.TaskManagementItem{{ID: card.ID}}})
	rr := request("POST", "/api/task-management/preview", string(payload))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"blocked_reason":"task_active"`) {
		t.Fatalf("preparing review not guarded: %d %s", rr.Code, rr.Body)
	}
	rr = request("POST", "/api/task-management/apply", string(payload))
	if rr.Code != 409 {
		t.Fatalf("preparing review deleted: %d %s", rr.Code, rr.Body)
	}
}
