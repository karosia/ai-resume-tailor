package jd

import (
	"context"
	"testing"

	"ai-resume-tailor/internal/llm"
)

type fakeCompleter struct {
	content string
	err     error
}

func (f *fakeCompleter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.content, Provider: "fake", Model: "fake"}, nil
}

func TestAnalyze_ParsesAndNormalizes(t *testing.T) {
	// Note the fence, the duplicate "Go"/"go", and the empty keyword — all of
	// which normalize() must clean up.
	canned := "```json\n" + `{
      "title":"Senior Backend Engineer",
      "seniority":"Senior",
      "required_skills":["Go","go","NATS"],
      "nice_to_have":["Kubernetes"],
      "keywords":["gRPC",""],
      "responsibilities":["Build services"]
    }` + "\n```"

	a := NewAnalyzer(&fakeCompleter{content: canned}, nil)
	jd, err := a.Analyze(context.Background(), "some jd text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jd.Seniority != "senior" {
		t.Fatalf("seniority not lowercased: %q", jd.Seniority)
	}
	if len(jd.RequiredSkills) != 2 { // "Go" and "go" collapse to one
		t.Fatalf("expected 2 required skills after dedupe, got %v", jd.RequiredSkills)
	}
	if len(jd.Keywords) != 1 { // empty string dropped
		t.Fatalf("expected 1 keyword after cleaning, got %v", jd.Keywords)
	}
}

func TestAnalyze_EmptyRejected(t *testing.T) {
	a := NewAnalyzer(&fakeCompleter{content: "{}"}, nil)
	if _, err := a.Analyze(context.Background(), "   "); err == nil {
		t.Fatal("expected error for empty JD text")
	}
}

func TestAnalyze_NoTermsRejected(t *testing.T) {
	// Valid JSON, but nothing usable to match on.
	a := NewAnalyzer(&fakeCompleter{content: `{"title":"X","required_skills":[],"keywords":[]}`}, nil)
	if _, err := a.Analyze(context.Background(), "real jd"); err == nil {
		t.Fatal("expected error when no skills or keywords extracted")
	}
}
