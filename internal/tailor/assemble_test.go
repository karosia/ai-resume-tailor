package tailor

import (
	"context"
	"testing"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/llm"
	"ai-resume-tailor/internal/resume"
)

func sourceItems() []resume.Item {
	return []resume.Item{
		{ID: "exp-1", Type: resume.ItemExperience, Company: "Northwind",
			Content: "Raised uptime from 99.2% to 99.98%", Metrics: []string{"99.2%", "99.98%"}},
		{ID: "ach-1", Type: resume.ItemAchievement,
			Content: "Handled 2,000 requests/second at peak", Metrics: []string{"2,000"}},
	}
}

func TestVerify_CleanPasses(t *testing.T) {
	tl := &Tailored{
		Sections: []Section{{
			Heading: "Experience",
			Bullets: []Bullet{
				{ItemID: "exp-1", Text: "Improved uptime from 99.2% to 99.98%"},
				{ItemID: "ach-1", Text: "Served 2,000 requests per second at peak"},
			},
		}},
	}
	if vs := Verify(tl, sourceItems()); len(vs) != 0 {
		t.Fatalf("expected no violations, got %+v", vs)
	}
}

func TestVerify_FabricatedNumberFlagged(t *testing.T) {
	// Source says 99.98%; the bullet inflates it to 99.99%.
	tl := &Tailored{
		Sections: []Section{{
			Heading: "Experience",
			Bullets: []Bullet{{ItemID: "exp-1", Text: "Achieved 99.99% uptime"}},
		}},
	}
	vs := Verify(tl, sourceItems())
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(vs), vs)
	}
	if vs[0].ItemID != "exp-1" {
		t.Fatalf("wrong item flagged: %+v", vs[0])
	}
}

func TestVerify_CommaNormalization(t *testing.T) {
	// Source has "2,000"; bullet writes it as "2000". Same figure -> no violation.
	tl := &Tailored{
		Sections: []Section{{
			Heading: "X",
			Bullets: []Bullet{{ItemID: "ach-1", Text: "Sustained 2000 requests/second"}},
		}},
	}
	if vs := Verify(tl, sourceItems()); len(vs) != 0 {
		t.Fatalf("2,000 and 2000 should match, got %+v", vs)
	}
}

func TestVerify_UnknownItemID(t *testing.T) {
	tl := &Tailored{
		Sections: []Section{{
			Heading: "X",
			Bullets: []Bullet{{ItemID: "exp-999", Text: "Did something"}},
		}},
	}
	vs := Verify(tl, sourceItems())
	if len(vs) != 1 || vs[0].Reason != "cites an unknown item id" {
		t.Fatalf("expected unknown-id violation, got %+v", vs)
	}
}

func TestVerify_SummaryNumberCheckedAgainstPool(t *testing.T) {
	// "5" appears in no source item.
	tl := &Tailored{Summary: "Engineer with 5 years scaling systems"}
	vs := Verify(tl, sourceItems())
	if len(vs) != 1 || vs[0].ItemID != "(summary)" {
		t.Fatalf("expected summary violation, got %+v", vs)
	}
}

// --- Assemble happy path with a fake LLM (no retry needed) ---

type fakeCompleter struct{ content string }

func (f *fakeCompleter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: f.content, Provider: "fake", Model: "fake"}, nil
}

func TestAssemble_CleanDraftNoRetry(t *testing.T) {
	canned := `{"summary":"Backend engineer","sections":[{"heading":"Experience","bullets":[{"item_id":"exp-1","text":"Improved uptime from 99.2% to 99.98%"}]}]}`
	a := NewAssembler(&fakeCompleter{content: canned}, nil)

	res, err := a.Assemble(context.Background(), sourceItems(), &jd.JD{Title: "Backend Engineer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Violations) != 0 {
		t.Fatalf("expected clean result, got violations: %+v", res.Violations)
	}
	if len(res.Tailored.Sections) != 1 {
		t.Fatalf("unexpected tailored output: %+v", res.Tailored)
	}
}
