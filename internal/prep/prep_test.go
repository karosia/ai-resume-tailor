package prep

import (
	"context"
	"strings"
	"testing"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/resume"
)

type fakeCompleter struct{ content string }

func (f *fakeCompleter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: f.content, Provider: "fake", Model: "fake"}, nil
}

func sourceItems() []resume.Item {
	return []resume.Item{
		{ID: "exp-1", Type: resume.ItemExperience, Content: "Built an event-driven platform with Go and NATS"},
		{ID: "ach-1", Type: resume.ItemAchievement, Content: "Cut p99 latency by 40%"},
	}
}

func TestGenerate_ParsesAndGrounds(t *testing.T) {
	canned := "```json\n" + `{"questions":[
      {"category":"technical","question":"How do you design an event-driven system?","answer":"At my last role I built one with Go and NATS...","item_ids":["exp-1"]},
      {"category":"behavioral","question":"Tell me about a performance win.","answer":"I cut p99 latency by 40%...","item_ids":["ach-1"]}
    ]}` + "\n```"

	g := NewGenerator(&fakeCompleter{content: canned}, nil)
	res, err := g.Generate(context.Background(), sourceItems(), &jd.JD{Title: "Backend Engineer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Prep.Questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(res.Prep.Questions))
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no warnings for valid item ids, got %+v", res.Warnings)
	}
}

func TestGenerate_FlagsInventedExperience(t *testing.T) {
	// The answer cites exp-999, which the candidate does not have.
	canned := `{"questions":[
      {"category":"technical","question":"Kafka experience?","answer":"I ran Kafka at scale...","item_ids":["exp-999"]}
    ]}`

	g := NewGenerator(&fakeCompleter{content: canned}, nil)
	res, err := g.Generate(context.Background(), sourceItems(), &jd.JD{Title: "X"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("expected 1 warning for invented experience, got %d: %+v", len(res.Warnings), res.Warnings)
	}
	if res.Warnings[0].ItemID != "exp-999" {
		t.Fatalf("wrong item flagged: %+v", res.Warnings[0])
	}
}

func TestGenerate_EmptyItemsRejected(t *testing.T) {
	g := NewGenerator(&fakeCompleter{content: "{}"}, nil)
	if _, err := g.Generate(context.Background(), nil, &jd.JD{}); err == nil {
		t.Fatal("expected error when there are no items to ground answers in")
	}
}

func TestRender_GroupsByCategory(t *testing.T) {
	p := &Prep{Questions: []Question{
		{Category: CategoryBehavioral, Question: "B?", Answer: "b."},
		{Category: CategoryTechnical, Question: "T?", Answer: "t."},
	}}
	out := Render(p)
	// Technical must appear before Behavioral (fixed ordering).
	ti := strings.Index(out, "## Technical")
	bi := strings.Index(out, "## Behavioral")
	if ti == -1 || bi == -1 || ti > bi {
		t.Fatalf("categories not ordered correctly:\n%s", out)
	}
}
