package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestTaskPreviewRoutesAreReadOnlyAndBounded(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "preview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	capture, err := repo.StartCaptureRun("capture", "entry", false)
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := repo.SaveExperiment(benchmark.Experiment{Name: "review", Task: benchmark.Task{Prompt: "Background"}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(&config.Config{}, repo, nil, nil).RegisterRoutes(mux)
	for _, tc := range []struct {
		method, path, contains string
		status                 int
	}{
		{"GET", "/api/task-previews/capture/" + capture.ID, `"traces":[]`, 200},
		{"GET", "/api/task-previews/experiment/" + experiment.ID, `"background":"Background"`, 200},
		{"GET", "/api/task-previews/capture/missing", "", 404},
		{"GET", "/api/task-previews/experiment/missing", "", 404},
		{"GET", "/api/task-previews/unknown/one", "", 404},
		{"GET", "/api/task-previews/capture/", "", 404},
		{"POST", "/api/task-previews/capture/" + capture.ID, "", 405},
	} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.status || !strings.Contains(rr.Body.String(), tc.contains) {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, rr.Code, rr.Body)
		}
	}
}
