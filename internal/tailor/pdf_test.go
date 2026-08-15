package tailor

import (
	"bytes"
	"testing"

	"ai-resume-tailor/internal/resume"
)

func sampleItems() []resume.Item {
	return []resume.Item{
		{ID: "exp-1", Type: resume.ItemExperience, Title: "Senior Backend Engineer", Company: "Acme", StartDate: "Jan 2022", EndDate: "May 2025"},
		{ID: "ach-1", Type: resume.ItemAchievement, Company: "Acme", StartDate: "Jan 2022", EndDate: "May 2025"},
		{ID: "exp-2", Type: resume.ItemExperience, Title: "Full Stack Developer", Company: "Globex", StartDate: "Sep 2025", EndDate: "Mar 2026"},
		{ID: "ski-1", Type: resume.ItemSkill},
	}
}

func TestRenderPDF_ProducesValidPDF(t *testing.T) {
	tl := &Tailored{
		Summary: "Backend engineer with experience in Go and distributed systems.",
		Sections: []Section{
			{
				Heading: "Experience",
				Bullets: []Bullet{
					{ItemID: "exp-1", Text: "Raised uptime from 99.2% to 99.98% by migrating a monolith to microservices, and this bullet is intentionally long so it wraps across multiple lines to exercise the hanging indent logic."},
					{ItemID: "exp-2", Text: "Built an event-driven delivery pipeline."},
				},
			},
			{
				Heading: "Skills",
				Bullets: []Bullet{{ItemID: "ski-1", Text: "Go, NATS, Kubernetes, PostgreSQL"}},
			},
		},
	}

	h := Header{
		Name:     "Alex Rivera",
		Location: "Vancouver, BC",
		Email:    "alex@example.com",
		GitHub:   "github.com/alex",
	}

	data, err := RenderPDF(tl, sampleItems(), h)
	if err != nil {
		t.Fatalf("RenderPDF error: %v", err)
	}
	if len(data) < 500 {
		t.Fatalf("PDF suspiciously small: %d bytes", len(data))
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("output does not start with the PDF magic header")
	}
	if !bytes.Contains(data, []byte("%%EOF")) {
		t.Fatalf("output missing PDF EOF marker")
	}
}

func TestRenderPDF_EmptyStillValid(t *testing.T) {
	data, err := RenderPDF(&Tailored{}, nil, Header{})
	if err != nil {
		t.Fatalf("RenderPDF error on empty input: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("empty resume did not produce a valid PDF")
	}
}

func TestGroupByCompany_GroupsAndOrders(t *testing.T) {
	tl := &Tailored{
		Sections: []Section{{
			Heading: "Experience",
			Bullets: []Bullet{
				{ItemID: "ach-1", Text: "Acme achievement"},
				{ItemID: "exp-2", Text: "Globex work"},
				{ItemID: "exp-1", Text: "Acme role work"},
				{ItemID: "ski-1", Text: "Go, NATS"},
			},
		}},
	}

	g := groupByCompany(tl, sampleItems())

	if len(g.Experience) != 2 {
		t.Fatalf("expected 2 company groups, got %d", len(g.Experience))
	}
	if g.Experience[0].Company != "Globex" {
		t.Fatalf("expected Globex first (most recent), got %s", g.Experience[0].Company)
	}
	var acme *ExperienceGroup
	for i := range g.Experience {
		if g.Experience[i].Company == "Acme" {
			acme = &g.Experience[i]
		}
	}
	if acme == nil || len(acme.Bullets) != 2 {
		t.Fatalf("Acme should have 2 bullets, got %+v", acme)
	}
	if acme.Role != "Senior Backend Engineer" {
		t.Fatalf("Acme role should come from the experience item, got %q", acme.Role)
	}
	if len(g.Other) != 1 || len(g.Other[0].Bullets) != 1 {
		t.Fatalf("expected 1 Other section with 1 bullet, got %+v", g.Other)
	}
}

func TestGroupByCompany_ExcludesEducation(t *testing.T) {
	items := []resume.Item{
		{ID: "exp-1", Type: resume.ItemExperience, Title: "Backend Engineer", Company: "Acme", StartDate: "2022", EndDate: "2025"},
		{ID: "edu-1", Type: resume.ItemEducation, Title: "BSc", Company: "State University", StartDate: "2014", EndDate: "2018"},
	}
	tl := &Tailored{
		Sections: []Section{{
			Heading: "Experience",
			Bullets: []Bullet{
				{ItemID: "exp-1", Text: "Built services"},
				{ItemID: "edu-1", Text: "BSc, State University"}, // must NOT become a company block
			},
		}},
	}

	g := groupByCompany(tl, items)

	// Only Acme should be an experience group — the university must not appear.
	if len(g.Experience) != 1 || g.Experience[0].Company != "Acme" {
		t.Fatalf("education leaked into experience: %+v", g.Experience)
	}
	// The education bullet falls through to Other.
	found := false
	for _, o := range g.Other {
		for _, b := range o.Bullets {
			if b == "BSc, State University" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("education bullet should fall through to Other section")
	}
}
