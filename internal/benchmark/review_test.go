package benchmark

import "testing"

func TestReviewConfigSupportsArbitraryScenariosAndBounds(t *testing.T) {
	c := ReviewConfig{TemplateName: "My arbitrary task", Points: []ReviewPoint{{ID: "my-question", Name: "Did it verify the initial assumption?"}}, Selected: []string{"my-question"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Selected = nil
	if err := c.Validate(); err == nil {
		t.Fatal("empty analysis selection accepted")
	}
	if err := c.ValidateDraft(); err != nil {
		t.Fatal("draft could not request suggestions")
	}
	c.Selected = []string{"unknown"}
	if err := c.Validate(); err == nil {
		t.Fatal("unknown selected point accepted")
	}
	c.Selected = []string{"my-question"}
	c.Points = append(c.Points, c.Points[0])
	if err := c.Validate(); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
	c.Points = c.Points[:1]
	c.Points[0].Metric = "execute_shell"
	if err := c.Validate(); err == nil {
		t.Fatal("unsupported automatic metric accepted")
	}
}
