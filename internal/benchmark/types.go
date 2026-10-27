// Package benchmark compares basic request metrics for Agent and model runs.
package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid experiment data")

// Legacy evaluation fields remain readable so existing experiment data is preserved.
type Criterion struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
}

type Task struct {
	Prompt      string      `json:"prompt"`
	Repository  string      `json:"repository"`
	BaseCommit  string      `json:"base_commit"`
	Environment string      `json:"environment"`
	Acceptance  string      `json:"acceptance"`
	TestCommand string      `json:"test_command"`
	Rubric      []Criterion `json:"rubric"`
}

type Experiment struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Notes         string         `json:"notes"`
	Task          Task           `json:"task"`
	Keep          bool           `json:"keep"`
	Archived      bool           `json:"archived"`
	Revision      int64          `json:"revision"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	RunCount      int            `json:"run_count"`
	Review        *ReviewConfig  `json:"review,omitempty"`
	ReviewSummary *ReviewSummary `json:"review_summary,omitempty"`
}

type Conditions struct {
	Agent         string `json:"agent"`
	AgentVersion  string `json:"agent_version"`
	Model         string `json:"model"`
	Configuration string `json:"configuration"`
	Environment   string `json:"environment"`
	BaseCommit    string `json:"base_commit"`
	Seed          string `json:"seed"`
}

type TestResults struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type CodeChanges struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

type Assessment struct {
	Diff          string             `json:"diff"`
	Verdict       string             `json:"verdict"` // unknown / pass / partial / fail
	Scores        map[string]float64 `json:"scores"`  // human ratings, 0..5; absent means unknown
	Tests         *TestResults       `json:"tests,omitempty"`
	Changes       *CodeChanges       `json:"changes,omitempty"`
	Interventions *int               `json:"interventions,omitempty"`
	ResultCommit  string             `json:"result_commit"`
	Artifacts     string             `json:"artifacts"`
	Verification  string             `json:"verification"`
	Notes         string             `json:"notes"`
}

type Run struct {
	ID           string     `json:"id"`
	ExperimentID string     `json:"experiment_id"`
	Name         string     `json:"name"`
	ReviewNotes  string     `json:"review_notes,omitempty"`
	Conditions   Conditions `json:"conditions"`
	Task         Task       `json:"task"` // immutable snapshot when the run is created
	TaskHash     string     `json:"task_hash"`
	State        string     `json:"state"` // planned / running / completed
	CaptureRunID string     `json:"capture_run_id"`
	TraceIDs     []string   `json:"trace_ids"`
	Assessment   Assessment `json:"assessment"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Revision     int64      `json:"revision"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func Hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (e *Experiment) Validate() error {
	if e.Review != nil {
		if err := e.Review.Validate(); err != nil {
			return err
		}
	}
	e.ReviewSummary = nil
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" || len(e.Name) > 512 || len(e.Notes) > 16000 || len(e.Task.Prompt) > 65536 || len(e.Task.Environment) > 16000 || len(e.Task.Acceptance) > 16000 || len(e.Task.TestCommand) > 8000 || len(e.Task.Repository) > 2048 || len(e.Task.BaseCommit) > 256 || len(e.Task.Rubric) > 20 {
		return fmt.Errorf("%w: experiment name is missing or fields are too large", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, c := range e.Task.Rubric {
		if c.ID == "" || len(c.ID) > 80 || c.Name == "" || len(c.Name) > 200 || c.Weight <= 0 || c.Weight > 100 || seen[c.ID] {
			return fmt.Errorf("%w: invalid rubric", ErrInvalid)
		}
		seen[c.ID] = true
	}
	return nil
}

// Normalize keeps optional collections usable by API consumers, including when
// reading documents created before empty collections were stored as arrays/maps.
func (r *Run) Normalize() {
	if r.TraceIDs == nil {
		r.TraceIDs = []string{}
	}
	if r.Assessment.Scores == nil {
		r.Assessment.Scores = map[string]float64{}
	}
}

func (r *Run) Validate() error {
	r.Normalize()
	r.Name = strings.TrimSpace(r.Name)
	r.Conditions.Agent = strings.TrimSpace(r.Conditions.Agent)
	r.Conditions.Model = strings.TrimSpace(r.Conditions.Model)
	if len(r.ReviewNotes) > 32000 || r.Name == "" || len(r.Name) > 512 || r.Conditions.Agent == "" || len(r.Conditions.Agent) > 200 || len(r.Conditions.Model) > 256 || len(r.Conditions.AgentVersion) > 200 || len(r.Conditions.Configuration) > 16000 || len(r.Conditions.Environment) > 16000 || len(r.Conditions.BaseCommit) > 256 || len(r.Conditions.Seed) > 200 || len(r.TraceIDs) > 100 {
		return fmt.Errorf("%w: invalid run conditions", ErrInvalid)
	}
	if r.State == "" {
		r.State = "planned"
	}
	if r.State != "planned" && r.State != "running" && r.State != "completed" {
		return fmt.Errorf("%w: invalid run state", ErrInvalid)
	}
	if r.Assessment.Verdict == "" {
		r.Assessment.Verdict = "unknown"
	}
	switch r.Assessment.Verdict {
	case "unknown", "pass", "partial", "fail":
	default:
		return fmt.Errorf("%w: invalid verdict", ErrInvalid)
	}
	rubric := map[string]bool{}
	for _, c := range r.Task.Rubric {
		rubric[c.ID] = true
	}
	for id, score := range r.Assessment.Scores {
		if !rubric[id] || score < 0 || score > 5 {
			return fmt.Errorf("%w: ratings must match this run's rubric and be 0..5", ErrInvalid)
		}
	}
	if v := r.Assessment.Tests; v != nil && (v.Passed < 0 || v.Failed < 0 || v.Skipped < 0) {
		return fmt.Errorf("%w: invalid test counts", ErrInvalid)
	}
	if v := r.Assessment.Changes; v != nil && (v.Files < 0 || v.Added < 0 || v.Deleted < 0) {
		return fmt.Errorf("%w: invalid diff counts", ErrInvalid)
	}
	if r.Assessment.Interventions != nil && *r.Assessment.Interventions < 0 {
		return fmt.Errorf("%w: invalid intervention count", ErrInvalid)
	}
	if len(r.Assessment.Diff) > 65536 || len(r.Assessment.Verification) > 32000 || len(r.Assessment.Notes) > 32000 || len(r.Assessment.Artifacts) > 16000 || len(r.Assessment.ResultCommit) > 256 {
		return fmt.Errorf("%w: assessment too large", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, id := range r.TraceIDs {
		if strings.TrimSpace(id) == "" || len(id) > 200 || seen[id] {
			return fmt.Errorf("%w: invalid/duplicate trace ID", ErrInvalid)
		}
		seen[id] = true
	}
	return nil
}
