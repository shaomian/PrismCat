package storage

import "github.com/shaomian/PrismCat/internal/benchmark"

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
