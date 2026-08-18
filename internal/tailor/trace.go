package tailor

import (
	"ai-resume-tailor/internal/jd"
	"ai-resume-tailor/internal/resume"
	"context"
	"fmt"

	aitracecause "github.com/karosia/ai-trace-cause"
	"github.com/karosia/ai-trace-cause/semantic"
)

// TraceResult holds the ids of the actions recorded for a tailored resume, so a
// caller can later ask the Trace to Explain any one of them. IncludedByItem maps
// a résumé item id to the "place_bullet" action(s) taken for it; Violations maps
// an item id to the "flag_violation" action recorded when a bullet's facts
// couldn't be traced to that item.
type TraceResult struct {
	IncludedByItem map[string][]string
	Violations     map[string][]string
}

// RecordCausalTrace records *why* the tailored resume looks the way it does, as
// a causal graph: for every bullet, its source item (Source) produced a JD
// requirement match (Observation) that supports a coverage fact (Fact), which is
// the basis of the decision to include the bullet (Decision), which caused it to
// be placed in a section (Action). Verification violations are recorded as their
// own decision→action chain, so a flagged figure is explainable too.
//
// It records the semantic model only; it does not alter the resume. The caller
// owns the Trace (and its store), and can call tr.Explain(actionID, depth) on any
// id in the returned TraceResult.
func RecordCausalTrace(
	ctx context.Context,
	tr *aitracecause.Trace,
	t *Tailored,
	items []resume.Item,
	j *jd.JD,
) (*TraceResult, error) {
	if tr == nil || t == nil {
		return nil, fmt.Errorf("tailor: RecordCausalTrace: nil trace or tailored")
	}

	byID := make(map[string]resume.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	// Which JD terms each item covers, so an included bullet can point at the
	// concrete requirement that justified it.
	match := Match(items, j)
	matchedByItem := make(map[string][]string, len(match.Ranked))
	scoreByItem := make(map[string]int, len(match.Ranked))
	for _, si := range match.Ranked {
		matchedByItem[si.Item.ID] = si.Matched
		scoreByItem[si.Item.ID] = si.Score
	}

	// Record each item once as a Source, reusing its id across bullets.
	sourceID := make(map[string]string, len(items))
	recordSource := func(it resume.Item) (string, error) {
		if id, ok := sourceID[it.ID]; ok {
			return id, nil
		}
		src, err := tr.RecordSource(ctx, semantic.Source{
			Kind: "resume_item",
			URI:  it.ID,
			Metadata: map[string]any{
				"title":   it.Title,
				"company": it.Company,
				"type":    string(it.Type),
			},
		})
		if err != nil {
			return "", fmt.Errorf("record source %s: %w", it.ID, err)
		}
		sourceID[it.ID] = src.ID
		return src.ID, nil
	}

	out := &TraceResult{
		IncludedByItem: map[string][]string{},
		Violations:     map[string][]string{},
	}

	// Violations are keyed by item id so we can annotate the matching bullet's
	// chain instead of recording it as a normal inclusion.
	violated := make(map[string][]Violation)
	for _, v := range Verify(t, items) {
		violated[v.ItemID] = append(violated[v.ItemID], v)
	}

	for _, sec := range t.Sections {
		for _, bl := range sec.Bullets {
			it, ok := byID[bl.ItemID]
			if !ok {
				// A bullet citing an unknown item is itself a violation; record a
				// standalone flag action with no source.
				if err := recordUnknownItem(ctx, tr, bl, sec.Heading, out); err != nil {
					return nil, err
				}
				continue
			}

			srcID, err := recordSource(it)
			if err != nil {
				return nil, err
			}

			// Source -> Observation: the JD requirement(s) this item matched.
			matched := matchedByItem[it.ID]
			reqValue := "general relevance"
			if len(matched) > 0 {
				reqValue = joinTerms(matched)
			}
			obs, err := tr.RecordObservation(ctx, semantic.Observation{
				Name:  "jd_requirement_match",
				Value: reqValue,
				Metadata: map[string]any{
					"role":  j.Title,
					"terms": matched,
				},
			})
			if err != nil {
				return nil, fmt.Errorf("record observation: %w", err)
			}
			if err := tr.Produced(ctx, srcID, obs.ID); err != nil {
				return nil, fmt.Errorf("produced: %w", err)
			}

			// Observation -> Fact: this item covers those requirements.
			conf := coverageConfidence(scoreByItem[it.ID], len(match.CoveredTerms))
			fact, err := tr.RecordFact(ctx, semantic.Fact{
				Statement:  fmt.Sprintf("item %q covers %s", it.ID, reqValue),
				Confidence: conf,
			})
			if err != nil {
				return nil, fmt.Errorf("record fact: %w", err)
			}
			if err := tr.Supports(ctx, obs.ID, fact.ID); err != nil {
				return nil, fmt.Errorf("supports: %w", err)
			}

			// If this bullet has a verification violation, the decision/action
			// reflect the flag rather than a clean inclusion.
			if vs := violated[bl.ItemID]; len(vs) > 0 {
				actID, err := recordFlagged(ctx, tr, fact.ID, bl, sec.Heading, vs)
				if err != nil {
					return nil, err
				}
				out.Violations[bl.ItemID] = append(out.Violations[bl.ItemID], actID)
				continue
			}

			// Fact -> Decision: choose to include this bullet.
			dec, err := tr.RecordDecision(ctx, semantic.Decision{
				Outcome:    "include_bullet",
				Rationale:  fmt.Sprintf("matches %s", reqValue),
				Confidence: conf,
			})
			if err != nil {
				return nil, fmt.Errorf("record decision: %w", err)
			}
			if err := tr.BasisOf(ctx, fact.ID, dec.ID); err != nil {
				return nil, fmt.Errorf("basisOf: %w", err)
			}

			// Decision -> Action: place the bullet in its section.
			act, err := tr.RecordAction(ctx, semantic.Action{
				Name:   "place_bullet",
				Target: sec.Heading,
				Parameters: map[string]any{
					"item_id": bl.ItemID,
					"text":    bl.Text,
				},
			})
			if err != nil {
				return nil, fmt.Errorf("record action: %w", err)
			}
			if err := tr.Caused(ctx, dec.ID, act.ID); err != nil {
				return nil, fmt.Errorf("caused: %w", err)
			}
			out.IncludedByItem[bl.ItemID] = append(out.IncludedByItem[bl.ItemID], act.ID)
		}
	}

	return out, nil
}

// recordFlagged records the decision/action for a bullet whose facts couldn't be
// verified against its source item. Confidence is high because the *flag* is
// certain, even though the bullet's claim is not.
func recordFlagged(
	ctx context.Context,
	tr *aitracecause.Trace,
	factID string,
	bl Bullet,
	section string,
	vs []Violation,
) (string, error) {
	reasons := make([]string, 0, len(vs))
	for _, v := range vs {
		reasons = append(reasons, v.Reason)
	}
	dec, err := tr.RecordDecision(ctx, semantic.Decision{
		Outcome:    "flag_unverifiable",
		Rationale:  fmt.Sprintf("claim not traceable to source: %s", joinTerms(reasons)),
		Confidence: 1.0,
	})
	if err != nil {
		return "", fmt.Errorf("record flag decision: %w", err)
	}
	if err := tr.BasisOf(ctx, factID, dec.ID); err != nil {
		return "", fmt.Errorf("flag basisOf: %w", err)
	}
	act, err := tr.RecordAction(ctx, semantic.Action{
		Name:   "flag_violation",
		Target: section,
		Parameters: map[string]any{
			"item_id": bl.ItemID,
			"text":    bl.Text,
			"reasons": reasons,
		},
	})
	if err != nil {
		return "", fmt.Errorf("record flag action: %w", err)
	}
	if err := tr.Caused(ctx, dec.ID, act.ID); err != nil {
		return "", fmt.Errorf("flag caused: %w", err)
	}
	return act.ID, nil
}

// recordUnknownItem records the flag chain for a bullet that cites an item id we
// don't have. There's no Source to anchor it, so we start at the Decision.
func recordUnknownItem(
	ctx context.Context,
	tr *aitracecause.Trace,
	bl Bullet,
	section string,
	out *TraceResult,
) error {
	dec, err := tr.RecordDecision(ctx, semantic.Decision{
		Outcome:    "flag_unknown_item",
		Rationale:  fmt.Sprintf("bullet cites unknown item %q", bl.ItemID),
		Confidence: 1.0,
	})
	if err != nil {
		return fmt.Errorf("record unknown-item decision: %w", err)
	}
	act, err := tr.RecordAction(ctx, semantic.Action{
		Name:   "flag_violation",
		Target: section,
		Parameters: map[string]any{
			"item_id": bl.ItemID,
			"text":    bl.Text,
			"reason":  "unknown item id",
		},
	})
	if err != nil {
		return fmt.Errorf("record unknown-item action: %w", err)
	}
	if err := tr.Caused(ctx, dec.ID, act.ID); err != nil {
		return fmt.Errorf("unknown-item caused: %w", err)
	}
	out.Violations[bl.ItemID] = append(out.Violations[bl.ItemID], act.ID)
	return nil
}

// coverageConfidence turns an item's raw match score into a [0,1] confidence:
// the share of the JD's covered terms that this one item accounts for, capped at
// 1. Items that matched nothing still get a small floor so the chain isn't
// recorded with zero confidence.
func coverageConfidence(itemScore, totalCovered int) float64 {
	if totalCovered <= 0 {
		return 0.1
	}
	c := float64(itemScore) / float64(totalCovered)
	if c > 1 {
		c = 1
	}
	if c < 0.1 {
		c = 0.1
	}
	return c
}

// joinTerms joins terms for a human-readable rationale without pulling in extra
// deps; empty slices become "(none)".
func joinTerms(terms []string) string {
	if len(terms) == 0 {
		return "(none)"
	}
	out := terms[0]
	for _, t := range terms[1:] {
		out += ", " + t
	}
	return out
}
