// Package tailor matches resume items against an analyzed job description and
// (in a later milestone) assembles a tailored resume under a no-fabrication
// guardrail. This file holds the matching half: it is deterministic and
// read-only — no LLM calls, so there is no fabrication risk here.
package tailor

import (
	"sort"
	"strings"

	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/resume"
)

// ScoredItem is a resume item paired with how well it covers the JD's terms.
type ScoredItem struct {
	Item    resume.Item
	Score   int      // number of distinct JD terms this item covers
	Matched []string // which JD terms it covered
}

// MatchResult is the outcome of matching all items against a JD.
//
// CoveragePercent is LITERAL keyword coverage — the share of the JD's terms
// covered by at least one resume item. It is NOT a prediction of any real ATS
// system's score (those are proprietary and unknowable); it's an honest,
// explainable signal of how well your verified experience lines up with the
// posting's language.
type MatchResult struct {
	Ranked          []ScoredItem
	CoveredTerms    []string
	MissingTerms    []string
	CoveragePercent int
}

// Match scores each item by how many JD terms it covers, ranks them, and
// reports overall coverage plus which terms are missing entirely.
func Match(items []resume.Item, j *jd.JD) MatchResult {
	terms := dedupeTerms(append(append([]string{}, j.RequiredSkills...), j.Keywords...))
	covered := make(map[string]bool)

	ranked := make([]ScoredItem, 0, len(items))
	for _, it := range items {
		hay := normalize(itemText(it))
		var matched []string
		for _, term := range terms {
			if covers(hay, term) {
				matched = append(matched, term)
				covered[strings.ToLower(term)] = true
			}
		}
		if len(matched) > 0 {
			ranked = append(ranked, ScoredItem{Item: it, Score: len(matched), Matched: matched})
		}
	}

	// Highest score first; stable so equal scores keep their input order.
	sort.SliceStable(ranked, func(i, k int) bool {
		return ranked[i].Score > ranked[k].Score
	})

	var coveredTerms, missing []string
	for _, term := range terms {
		if covered[strings.ToLower(term)] {
			coveredTerms = append(coveredTerms, term)
		} else {
			missing = append(missing, term)
		}
	}

	pct := 0
	if len(terms) > 0 {
		pct = len(coveredTerms) * 100 / len(terms)
	}

	return MatchResult{
		Ranked:          ranked,
		CoveredTerms:    coveredTerms,
		MissingTerms:    missing,
		CoveragePercent: pct,
	}
}

// itemText gathers the searchable text of an item: its title, company,
// content, and skill tags.
func itemText(item resume.Item) string {
	parts := append([]string{item.Title, item.Company, item.Content}, item.Skills...)
	return strings.Join(parts, " ")
}

// normalize lowercases s and turns every run of non-alphanumeric characters
// into a single space, then pads the whole string with spaces. The padding
// lets covers() match on word boundaries: searching for " go " won't match
// inside "google".
func normalize(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		if isAlnum(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	if !prevSpace {
		b.WriteByte(' ')
	}
	return b.String()
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// covers reports whether the (already normalized) item text contains the given
// term as a whole word / phrase. Multi-word terms like "distributed systems"
// work because normalize collapses their internal punctuation to single spaces
// too.
func covers(normHay, term string) bool {
	nt := strings.TrimSpace(normalize(term))
	if nt == "" {
		return false
	}
	return strings.Contains(normHay, " "+nt+" ")
}

func dedupeTerms(terms []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}
