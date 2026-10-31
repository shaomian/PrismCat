package benchmark

import "sort"

type Sample struct {
	ID                   string
	TraceID              string
	CreatedAt            int64
	Latency              int64
	Status               int
	Error                bool
	Incomplete           bool
	Input, Output, Total *int64
}

type Metrics struct {
	Requests     int      `json:"requests"`
	Errors       int      `json:"errors"`
	Incomplete   int      `json:"incomplete"`
	SpanMS       *int64   `json:"span_ms"`
	InputTokens  *int64   `json:"input_tokens"`
	OutputTokens *int64   `json:"output_tokens"`
	TotalTokens  *int64   `json:"total_tokens"`
	UsageKnown   int      `json:"usage_known"`
	TraceIDs     []string `json:"trace_ids"`
	LogIDs       []string `json:"log_ids"`
	Sampled      bool     `json:"sampled"`
}

// Accumulator computes metadata statistics one row at a time. Only membership
// IDs are retained, for frozen review evidence and overlapping-run warnings.
// The zero value is ready to use.
type Accumulator struct {
	metrics    Metrics
	seen       map[string]bool
	traces     map[string]bool
	begin, end int64
	completed  bool
	summary    bool
}

// NewSummaryAccumulator consumes unique database rows without retaining their IDs.
// Trace IDs are a bounded display summary; overlap checks use database membership.
func NewSummaryAccumulator() *Accumulator { return &Accumulator{summary: true} }

func (a *Accumulator) Count() int { return a.metrics.Requests }

func (a *Accumulator) Add(s Sample) {
	if a.traces == nil {
		if !a.summary {
			a.seen = map[string]bool{}
		}
		a.traces = map[string]bool{}
		a.metrics.LogIDs = []string{}
	}
	if !a.summary && a.seen[s.ID] {
		return
	}
	if !a.summary {
		a.seen[s.ID] = true
	}
	m := &a.metrics
	if m.Requests == 0 || s.CreatedAt < a.begin {
		a.begin = s.CreatedAt
	}
	m.Requests++
	if !a.summary {
		m.LogIDs = append(m.LogIDs, s.ID)
	}
	if s.TraceID != "" && (!a.summary || len(a.traces) < 20) {
		a.traces[s.TraceID] = true
	}
	if s.Error || s.Status >= 400 {
		m.Errors++
	}
	if s.Incomplete || (s.Status == 0 && !s.Error) {
		m.Incomplete++
	} else {
		finish := s.CreatedAt + max(s.Latency, 0)
		if !a.completed || finish > a.end {
			a.end = finish
		}
		a.completed = true
	}
	sum := func(target **int64, value *int64) {
		if value != nil {
			if *target == nil {
				*target = new(int64)
			}
			**target += *value
		}
	}
	sum(&m.InputTokens, s.Input)
	sum(&m.OutputTokens, s.Output)
	total := s.Total
	if total == nil && s.Input != nil && s.Output != nil {
		v := *s.Input + *s.Output
		total = &v
	}
	sum(&m.TotalTokens, total)
	if s.Input != nil && s.Output != nil {
		m.UsageKnown++
	}
}

// Finish returns the final metrics; callers must not add more samples afterward.
func (a *Accumulator) Finish(sampled bool) Metrics {
	m := a.metrics
	m.Sampled = sampled
	if m.LogIDs == nil {
		m.LogIDs = []string{}
	}
	m.TraceIDs = make([]string, 0, len(a.traces))
	for id := range a.traces {
		m.TraceIDs = append(m.TraceIDs, id)
	}
	sort.Strings(m.TraceIDs)
	if a.completed {
		span := max(a.end-a.begin, 0)
		m.SpanMS = &span
	}
	return m
}

// Measure uses log metadata only. Missing usage stays unknown; overlapping
// requests contribute their wall-clock span, rather than summed latency.
func Measure(samples []Sample, sampled bool) Metrics {
	var accumulator Accumulator
	for _, sample := range samples {
		accumulator.Add(sample)
	}
	return accumulator.Finish(sampled)
}

type Result struct {
	Run      Run      `json:"run"`
	Metrics  Metrics  `json:"metrics"`
	Warnings []string `json:"warnings"`
}

type Comparison struct {
	Experiment Experiment `json:"experiment"`
	Results    []Result   `json:"results"`
	Warnings   []string   `json:"warnings"`
}

func Compare(experiment Experiment, results []Result) Comparison {
	c := Comparison{Experiment: experiment, Results: results, Warnings: []string{}}
	hashes, owners := map[string]bool{}, map[string]string{}
	overlap := false
	for i := range c.Results {
		r := &c.Results[i]
		r.Warnings = []string{}
		hashes[r.Run.TaskHash] = true
		if r.Metrics.Requests == 0 {
			r.Warnings = append(r.Warnings, "no_logs")
		}
		if r.Metrics.Sampled {
			r.Warnings = append(r.Warnings, "sampled")
		}
		if r.Metrics.UsageKnown < r.Metrics.Requests {
			r.Warnings = append(r.Warnings, "partial_usage")
		}
		if r.Metrics.Incomplete > 0 || r.Run.State == "running" {
			r.Warnings = append(r.Warnings, "run_not_completed")
		}
		for _, id := range r.Metrics.LogIDs {
			if owner := owners[id]; owner != "" && owner != r.Run.ID {
				overlap = true
			}
			owners[id] = r.Run.ID
		}
	}
	if len(hashes) > 1 {
		c.Warnings = append(c.Warnings, "task_changed")
	}
	if overlap {
		c.Warnings = append(c.Warnings, "overlapping_logs")
	}
	return c
}
