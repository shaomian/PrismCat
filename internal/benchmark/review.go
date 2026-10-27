package benchmark

import (
	"fmt"
	"strings"
	"time"
)

// ReviewConfig is a snapshot: editing a reusable template never changes a card.
type ReviewPoint struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	Metric       string `json:"metric,omitempty"`
}
type ReviewConfig struct {
	TemplateID   string        `json:"template_id"`
	TemplateName string        `json:"template_name"`
	Points       []ReviewPoint `json:"points"`
	Selected     []string      `json:"selected"`
	Instructions string        `json:"instructions"`
}

func (c *ReviewConfig) Validate() error      { return c.validate(true) }
func (c *ReviewConfig) ValidateDraft() error { return c.validate(false) }
func (c *ReviewConfig) validate(required bool) error {
	if len(c.TemplateID) > 100 || len(c.TemplateName) > 300 || len(c.Instructions) > 8000 || len(c.Points) > 30 || (required && len(c.Points) == 0) || len(c.Selected) > 20 || (required && len(c.Selected) == 0) {
		return fmt.Errorf("%w: choose 1–20 observation points (up to 30 available)", ErrInvalid)
	}
	ids := map[string]bool{}
	for i := range c.Points {
		p := &c.Points[i]
		p.Name = strings.TrimSpace(p.Name)
		if p.ID == "" || len(p.ID) > 100 || p.Name == "" || len(p.Name) > 300 || len(p.Instructions) > 2000 || ids[p.ID] {
			return fmt.Errorf("%w: invalid or duplicate observation point", ErrInvalid)
		}
		switch p.Metric {
		case "", "span_ms", "total_tokens", "input_tokens", "output_tokens", "requests", "errors":
		default:
			return fmt.Errorf("%w: unknown automatic metric", ErrInvalid)
		}
		ids[p.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range c.Selected {
		if !ids[id] || seen[id] {
			return fmt.Errorf("%w: invalid selected observation point", ErrInvalid)
		}
		seen[id] = true
	}
	return nil
}

type ReviewSummary struct {
	TemplateName string `json:"template_name"`
	PointCount   int    `json:"point_count"`
	Status       string `json:"status"`
}
type ReviewTemplate struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Config      ReviewConfig `json:"config"`
	Archived    bool         `json:"archived"`
	Revision    int64        `json:"revision"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}
type ReviewTemplateSummary struct {
	Archived    bool   `json:"archived"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PointCount  int    `json:"point_count"`
	Revision    int64  `json:"revision"`
}
type ReviewTask struct {
	RunID    string   `json:"run_id"`
	Name     string   `json:"name"`
	Agent    string   `json:"agent"`
	Model    string   `json:"model"`
	Revision int64    `json:"revision"`
	Prompt   string   `json:"prompt"`
	Notes    string   `json:"notes"`
	Metrics  Metrics  `json:"metrics"`
	Warnings []string `json:"warnings"`
}
type ReviewFinding struct {
	RunID       string   `json:"run_id"`
	Value       string   `json:"value"`
	Explanation string   `json:"explanation"`
	Source      string   `json:"source"` // statistics / logs / inference / unknown
	Evidence    []string `json:"evidence"`
}
type ReviewObservation struct {
	PointID    string          `json:"point_id"`
	Comparison string          `json:"comparison"`
	Findings   []ReviewFinding `json:"findings"`
}
type ReviewOutput struct {
	Summary         string              `json:"summary"`
	Observations    []ReviewObservation `json:"observations"`
	Recommendations []string            `json:"recommendations"`
}
type ReviewReport struct {
	Background   string         `json:"background"`
	ID           string         `json:"id"`
	ExperimentID string         `json:"experiment_id"`
	ParentID     string         `json:"parent_id"`
	Question     string         `json:"question"`
	Status       string         `json:"status"`
	Config       ReviewConfig   `json:"config"`
	Tasks        []ReviewTask   `json:"tasks"`
	Output       *ReviewOutput  `json:"output,omitempty"`
	Error        string         `json:"error"`
	Model        string         `json:"model"`
	Usage        map[string]any `json:"usage,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
}
type ReviewReportSummary struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id"`
	Question  string    `json:"question"`
	Status    string    `json:"status"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
}
