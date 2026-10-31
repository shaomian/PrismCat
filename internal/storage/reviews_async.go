package storage

import (
	"context"
	"github.com/shaomian/PrismCat/internal/benchmark"
)

func (a *AsyncRepository) reviewStore() (ReviewRepository, error) {
	r, err := a.experimentStore()
	if err != nil {
		return nil, err
	}
	v, ok := r.(ReviewRepository)
	if !ok {
		return nil, benchmark.ErrInvalid
	}
	return v, nil
}
func (a *AsyncRepository) ListReviewTemplates(q string, o, l int) ([]benchmark.ReviewTemplateSummary, int64, error) {
	r, e := a.reviewStore()
	if e != nil {
		return nil, 0, e
	}
	return r.ListReviewTemplates(q, o, l)
}
func (a *AsyncRepository) GetReviewTemplate(id string) (benchmark.ReviewTemplate, error) {
	r, e := a.reviewStore()
	if e != nil {
		return benchmark.ReviewTemplate{}, e
	}
	return r.GetReviewTemplate(id)
}
func (a *AsyncRepository) SaveReviewTemplate(v benchmark.ReviewTemplate) (benchmark.ReviewTemplate, error) {
	r, e := a.reviewStore()
	if e != nil {
		return v, e
	}
	return r.SaveReviewTemplate(v)
}
func (a *AsyncRepository) ListReviewReports(id string, o, l int) ([]benchmark.ReviewReportSummary, int64, error) {
	r, e := a.reviewStore()
	if e != nil {
		return nil, 0, e
	}
	return r.ListReviewReports(id, o, l)
}
func (a *AsyncRepository) GetReviewReport(id string) (benchmark.ReviewReport, error) {
	r, e := a.reviewStore()
	if e != nil {
		return benchmark.ReviewReport{}, e
	}
	return r.GetReviewReport(id)
}
func (a *AsyncRepository) CreateReviewReport(v benchmark.ReviewReport) error {
	r, e := a.reviewStore()
	if e != nil {
		return e
	}
	return r.CreateReviewReport(v)
}
func (a *AsyncRepository) FinishReviewReport(v benchmark.ReviewReport) error {
	r, e := a.reviewStore()
	if e != nil {
		return e
	}
	return r.FinishReviewReport(v)
}
func (a *AsyncRepository) DeleteReviewReport(id, experimentID string) error {
	r, e := a.reviewStore()
	if e != nil {
		return e
	}
	return r.DeleteReviewReport(id, experimentID)
}

func (a *AsyncRepository) snapshotStore() (ReviewSnapshotRepository, error) {
	r, e := a.reviewStore()
	if e != nil {
		return nil, e
	}
	v, ok := r.(ReviewSnapshotRepository)
	if !ok {
		return nil, benchmark.ErrInvalid
	}
	return v, nil
}

func (a *AsyncRepository) SupportsReviewSnapshots() bool {
	_, err := a.snapshotStore()
	return err == nil
}
func (a *AsyncRepository) PrepareReviewSnapshot(ctx context.Context, card string, revision int64, tasks []benchmark.ReviewTask) (ReviewSnapshot, error) {
	r, e := a.snapshotStore()
	if e != nil {
		return ReviewSnapshot{}, e
	}
	return r.PrepareReviewSnapshot(ctx, card, revision, tasks)
}
func (a *AsyncRepository) ReviewSnapshotRequests(ctx context.Context, id, run string, cursor int, query string, errorsOnly bool, limit int) ([]ReviewSnapshotRequest, error) {
	r, e := a.snapshotStore()
	if e != nil {
		return nil, e
	}
	return r.ReviewSnapshotRequests(ctx, id, run, cursor, query, errorsOnly, limit)
}
func (a *AsyncRepository) ReviewSnapshotSamples(ctx context.Context, id, run string, positions []int) (map[int]string, error) {
	r, e := a.snapshotStore()
	if e != nil {
		return nil, e
	}
	return r.ReviewSnapshotSamples(ctx, id, run, positions)
}
func (a *AsyncRepository) ReviewSnapshotMembers(ctx context.Context, id, run string, ids []string) (map[string]bool, error) {
	r, e := a.snapshotStore()
	if e != nil {
		return nil, e
	}
	return r.ReviewSnapshotMembers(ctx, id, run, ids)
}
func (a *AsyncRepository) ReviewSnapshotOverlap(ctx context.Context, id string) (bool, error) {
	r, e := a.snapshotStore()
	if e != nil {
		return false, e
	}
	return r.ReviewSnapshotOverlap(ctx, id)
}
func (a *AsyncRepository) DiscardReviewSnapshot(id string) error {
	r, e := a.snapshotStore()
	if e != nil {
		return e
	}
	return r.DiscardReviewSnapshot(id)
}
