package tailor

import (
	"fmt"
	"sort"
	"strings"

	"ai-resume-tailor/internal/resume"
)

// Header holds the candidate's contact facts, shown at the top of the resume.
// These live outside the tailored items (the model never sees or invents them);
// the caller fills them from configuration/environment.
type Header struct {
	Name     string
	Email    string
	Phone    string
	Location string
	LinkedIn string
	GitHub   string
}

// contactLine assembles the single contact line under the name, joining only
// the fields that are set with a middle dot.
func (h Header) contactLine() string {
	var parts []string
	for _, p := range []string{h.Location, h.Email, h.Phone, h.LinkedIn, h.GitHub} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// dateRange formats a company header's dates as "Start – End", tolerating
// missing pieces.
func dateRange(start, end string) string {
	switch {
	case start != "" && end != "":
		return start + " – " + end
	case start != "":
		return start + " – Present"
	case end != "":
		return end
	default:
		return ""
	}
}

// ExperienceGroup is one company's block in the rendered resume: a header
// (company, role, dates) followed by the tailored bullets that belong to it.
// Grouping is done deterministically from the source items — the model never
// decides company names or dates, so they can't be invented or reordered.
type ExperienceGroup struct {
	Company string
	Role    string
	Start   string
	End     string
	Bullets []string // tailored bullet text, in the model's order within the company
}

// OtherSection carries bullets that don't belong to a company (e.g. a skills
// summary), preserving the model's original section heading.
type OtherSection struct {
	Heading string
	Bullets []string
}

// grouped is the full layout the renderers consume: company blocks plus any
// non-company sections.
type grouped struct {
	Experience []ExperienceGroup
	Other      []OtherSection
}

// groupByCompany walks the tailored bullets and buckets them by the company of
// their source item. Bullets whose item has a company become experience groups;
// the rest stay under their original section heading. Company blocks are ordered
// most-recent-first by end date.
func groupByCompany(t *Tailored, items []resume.Item) grouped {
	byID := make(map[string]resume.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	// Preserve first-seen company order while collecting, then sort by date.
	var order []string
	groups := make(map[string]*ExperienceGroup)
	var other []OtherSection

	for _, sec := range t.Sections {
		var loose []string // bullets in this section with no company
		for _, bl := range sec.Bullets {
			it, ok := byID[bl.ItemID]
			// Only role-style items (experience/achievement/project) group by
			// company. Education has a school in its Company field but is not a
			// job, so it must not become an experience block; skills/education
			// and unknown ids fall through to their original section.
			if !ok || it.Company == "" || !isExperienceType(it.Type) {
				loose = append(loose, bl.Text)
				continue
			}
			g, exists := groups[it.Company]
			if !exists {
				g = &ExperienceGroup{
					Company: it.Company,
					Role:    roleFor(it, byID, items),
					Start:   it.StartDate,
					End:     it.EndDate,
				}
				groups[it.Company] = g
				order = append(order, it.Company)
			}
			// Fill in role/dates from the best-available item for this company
			// (an "experience" item is preferred as the source of the header).
			if g.Role == "" {
				g.Role = it.Title
			}
			if g.Start == "" {
				g.Start = it.StartDate
			}
			if g.End == "" {
				g.End = it.EndDate
			}
			g.Bullets = append(g.Bullets, bl.Text)
		}
		if len(loose) > 0 {
			other = append(other, OtherSection{Heading: sec.Heading, Bullets: loose})
		}
	}

	exp := make([]ExperienceGroup, 0, len(order))
	for _, company := range order {
		exp = append(exp, *groups[company])
	}
	sort.SliceStable(exp, func(i, j int) bool {
		return moreRecent(exp[i].End, exp[j].End)
	})

	return grouped{Experience: exp, Other: other}
}

// isExperienceType reports whether an item type belongs in the company-grouped
// experience section. Education is deliberately excluded: a school in the
// Company field is not an employer.
func isExperienceType(t resume.ItemType) bool {
	switch t {
	case resume.ItemExperience, resume.ItemAchievement, resume.ItemProject:
		return true
	default:
		return false
	}
}

// roleFor picks the job title to show for a company header. An item of type
// "experience" holds the actual role, so prefer that; otherwise fall back to the
// current item's own title.
func roleFor(cur resume.Item, byID map[string]resume.Item, items []resume.Item) string {
	for _, it := range items {
		if it.Company == cur.Company && it.Type == resume.ItemExperience && it.Title != "" {
			return it.Title
		}
	}
	return cur.Title
}

// moreRecent reports whether end date a is more recent than b. Dates are free
// text ("Mar 2026", "Present", ""), so we can't parse reliably; we use simple,
// stable heuristics: "Present"/"Current"/empty sort newest, otherwise compare
// the trailing 4-digit year, falling back to string order.
func moreRecent(a, b string) bool {
	ra, rb := recencyKey(a), recencyKey(b)
	return ra > rb
}

func recencyKey(end string) int {
	switch normalizeEnd(end) {
	case "present", "current", "now", "":
		return 999999 // ongoing roles sort to the top
	}
	// Pull a 4-digit year out of the string if present.
	year := 0
	digits := 0
	for i := 0; i < len(end); i++ {
		c := end[i]
		if c >= '0' && c <= '9' {
			year = year*10 + int(c-'0')
			digits++
			if digits == 4 {
				break
			}
		} else {
			year, digits = 0, 0
		}
	}
	if digits == 4 {
		return year
	}
	return 0
}

func normalizeEnd(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != ' ' {
			out = append(out, c)
		}
	}
	return string(out)
}

// Render produces the tailored resume as Markdown: a contact header, the
// summary, company-grouped experience, and any remaining sections. Grouping and
// ordering are deterministic (see groupByCompany) so nothing in the layout can
// be fabricated by the model.
func Render(t *Tailored, items []resume.Item, h Header) string {
	var b strings.Builder

	// --- Header ---
	if h.Name != "" {
		fmt.Fprintf(&b, "# %s\n", h.Name)
	}
	if cl := h.contactLine(); cl != "" {
		fmt.Fprintf(&b, "%s\n", cl)
	}
	if h.Name != "" || h.contactLine() != "" {
		b.WriteString("\n")
	}

	// --- Summary ---
	if t.Summary != "" {
		b.WriteString(t.Summary)
		b.WriteString("\n\n")
	}

	g := groupByCompany(t, items)

	// --- Professional Experience (company-grouped) ---
	if len(g.Experience) > 0 {
		b.WriteString("## Professional Experience\n\n")
		for _, e := range g.Experience {
			// "**Company** — Role" with a right-hand date range.
			line := "**" + e.Company + "**"
			if e.Role != "" {
				line += " — " + e.Role
			}
			if dr := dateRange(e.Start, e.End); dr != "" {
				line += "  \n_" + dr + "_"
			}
			fmt.Fprintf(&b, "%s\n", line)
			for _, bl := range e.Bullets {
				fmt.Fprintf(&b, "- %s\n", bl)
			}
			b.WriteString("\n")
		}
	}

	// --- Other sections (skills, etc.) ---
	for _, s := range g.Other {
		fmt.Fprintf(&b, "## %s\n", s.Heading)
		for _, bl := range s.Bullets {
			fmt.Fprintf(&b, "- %s\n", bl)
		}
		b.WriteString("\n")
	}

	return b.String()
}
