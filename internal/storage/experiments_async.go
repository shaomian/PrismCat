package storage

import (
	"context"
	"errors"
	"github.com/shaomian/PrismCat/internal/benchmark"
)

func (a *AsyncRepository) experimentStore() (ExperimentRepository, error) {
	r, ok := a.inner.(ExperimentRepository)
	if !ok {
		return nil, errors.New("experiments are unavailable")
	}
	return r, nil
}
func (a *AsyncRepository) GetTraceMetadataDocument(id string) (TraceMetadata, error) {
	r, e := a.experimentStore()
	if e != nil {
		return TraceMetadata{}, e
	}
	return r.GetTraceMetadataDocument(id)
}
func (a *AsyncRepository) SaveTraceMetadata(m TraceMetadata) (TraceMetadata, error) {
	r, e := a.experimentStore()
	if e != nil {
		return m, e
	}
	return r.SaveTraceMetadata(m)
}
func (a *AsyncRepository) ListExperiments(q string, b bool, o, l int) ([]benchmark.Experiment, int64, error) {
	r, e := a.experimentStore()
	if e != nil {
		return nil, 0, e
	}
	return r.ListExperiments(q, b, o, l)
}
func (a *AsyncRepository) GetExperiment(id string) (benchmark.Experiment, error) {
	r, e := a.experimentStore()
	if e != nil {
		return benchmark.Experiment{}, e
	}
	return r.GetExperiment(id)
}
func (a *AsyncRepository) SaveExperiment(v benchmark.Experiment) (benchmark.Experiment, error) {
	r, e := a.experimentStore()
	if e != nil {
		return v, e
	}
	return r.SaveExperiment(v)
}
func (a *AsyncRepository) ListExperimentRuns(id string, o, l int) ([]benchmark.Run, int64, error) {
	r, e := a.experimentStore()
	if e != nil {
		return nil, 0, e
	}
	return r.ListExperimentRuns(id, o, l)
}
func (a *AsyncRepository) GetExperimentRun(id string) (benchmark.Run, error) {
	r, e := a.experimentStore()
	if e != nil {
		return benchmark.Run{}, e
	}
	return r.GetExperimentRun(id)
}
func (a *AsyncRepository) SaveExperimentRun(v benchmark.Run) (benchmark.Run, error) {
	r, e := a.experimentStore()
	if e != nil {
		return v, e
	}
	return r.SaveExperimentRun(v)
}
func (a *AsyncRepository) StartExperimentCapture(id string, v int64, u string) (benchmark.Run, error) {
	r, e := a.experimentStore()
	if e != nil {
		return benchmark.Run{}, e
	}
	return r.StartExperimentCapture(id, v, u)
}
func (a *AsyncRepository) StopExperimentCapture(id string, v int64) (benchmark.Run, error) {
	r, e := a.experimentStore()
	if e != nil {
		return benchmark.Run{}, e
	}
	return r.StopExperimentCapture(id, v)
}
func (a *AsyncRepository) ExperimentLogs(ctx context.Context, id string, l int) ([]*RequestLog, error) {
	r, e := a.experimentStore()
	if e != nil {
		return nil, e
	}
	return r.ExperimentLogs(ctx, id, l)
}

func (a *AsyncRepository) MeasureExperimentRun(ctx context.Context, id string, limit int) (benchmark.Metrics, error) {
	r, err := a.experimentStore()
	if err != nil {
		return benchmark.Metrics{}, err
	}
	return MeasureExperimentRun(ctx, r, id, limit)
}

func (a *AsyncRepository) MeasureFullExperimentRun(ctx context.Context, id string) (benchmark.Metrics, error) {
	r, err := a.experimentStore()
	if err != nil {
		return benchmark.Metrics{}, err
	}
	return FullExperimentMetrics(ctx, r, id)
}

// SearchExperimentLogs 走窄接口断言,不强迫所有 ExperimentRepository 实现
// (含测试桩)都补上可选的检索能力。
func (a *AsyncRepository) SearchExperimentLogs(ctx context.Context, id, query string, errorsOnly bool, limit, offset int) ([]*RequestLog, error) {
	r, e := a.experimentStore()
	if e != nil {
		return nil, e
	}
	searcher, ok := r.(interface {
		SearchExperimentLogs(context.Context, string, string, bool, int, int) ([]*RequestLog, error)
	})
	if !ok {
		return nil, errors.New("task search unavailable")
	}
	return searcher.SearchExperimentLogs(ctx, id, query, errorsOnly, limit, offset)
}

// SearchReviewLogs 将冻结的报告成员范围原样传给存储层。
func (a *AsyncRepository) SearchReviewLogs(ctx context.Context, ids []string, query string, errorsOnly bool, limit, offset int) ([]*RequestLog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, ok := a.inner.(interface {
		SearchReviewLogs(context.Context, []string, string, bool, int, int) ([]*RequestLog, error)
	})
	if !ok {
		return nil, errors.New("review search unavailable")
	}
	return repo.SearchReviewLogs(ctx, ids, query, errorsOnly, limit, offset)
}
