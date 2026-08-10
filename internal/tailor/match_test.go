package tailor

import (
	"testing"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/resume"
)

func TestMatch_ScoresAndCoverage(t *testing.T) {
	items := []resume.Item{
		{ID: "exp-1", Type: resume.ItemExperience, Content: "Built an event-driven platform", Skills: []string{"Go", "NATS"}},
		{ID: "ach-1", Type: resume.ItemAchievement, Content: "Wrote Python data pipelines"},
	}
	j := &jd.JD{
		RequiredSkills: []string{"Go", "NATS"},
		Keywords:       []string{"Kubernetes"},
	}

	res := Match(items, j)

	// exp-1 covers Go + NATS (score 2); ach-1 covers nothing and is excluded.
	if len(res.Ranked) != 1 {
		t.Fatalf("expected 1 ranked item, got %d", len(res.Ranked))
	}
	if res.Ranked[0].Item.ID != "exp-1" || res.Ranked[0].Score != 2 {
		t.Fatalf("unexpected top item: %+v", res.Ranked[0])
	}

	// Go + NATS covered, Kubernetes missing => 2 of 3 = 66%.
	if res.CoveragePercent != 66 {
		t.Fatalf("expected 66%% coverage, got %d", res.CoveragePercent)
	}
	if len(res.MissingTerms) != 1 || res.MissingTerms[0] != "Kubernetes" {
		t.Fatalf("expected Kubernetes missing, got %v", res.MissingTerms)
	}
}

// TestMatch_WordBoundary guards against substring false positives: the JD term
// "Go" must NOT match inside "Google" or "Golang-adjacent" prose.
func TestMatch_WordBoundary(t *testing.T) {
	items := []resume.Item{
		{ID: "x", Type: resume.ItemExperience, Content: "Deployed on Google Cloud Platform"},
	}
	j := &jd.JD{RequiredSkills: []string{"Go"}}

	res := Match(items, j)

	if len(res.Ranked) != 0 {
		t.Fatalf("'Go' should not match inside 'Google'; got %+v", res.Ranked)
	}
	if res.CoveragePercent != 0 {
		t.Fatalf("expected 0%% coverage, got %d", res.CoveragePercent)
	}
}

// TestMatch_MultiWordTerm checks that phrase terms match across word boundaries.
func TestMatch_MultiWordTerm(t *testing.T) {
	items := []resume.Item{
		{ID: "x", Type: resume.ItemSkill, Content: "Deep experience with distributed systems and caching"},
	}
	j := &jd.JD{RequiredSkills: []string{"distributed systems"}}

	res := Match(items, j)
	if len(res.Ranked) != 1 || res.CoveragePercent != 100 {
		t.Fatalf("multi-word term should match; got ranked=%d coverage=%d", len(res.Ranked), res.CoveragePercent)
	}
}

func TestMatch_EmptyJDTerms(t *testing.T) {
	items := []resume.Item{{ID: "x", Type: resume.ItemSkill, Content: "Go"}}
	res := Match(items, &jd.JD{})
	if res.CoveragePercent != 0 || len(res.Ranked) != 0 {
		t.Fatalf("empty JD should yield no matches, got %+v", res)
	}
}
