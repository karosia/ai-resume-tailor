package tailor

import (
	"bytes"
	"fmt"

	"github.com/go-pdf/fpdf"

	"ai-resume-tailor/internal/resume"
)

// RenderPDF lays out a tailored resume as a single-column A4 PDF and returns the
// file bytes. Layout constants are chosen for a clean, ATS-friendly document:
// standard fonts, generous margins, no columns or graphics that parsers choke on.
// Experience is grouped by company (deterministically, from items); the header
// facts come from h, outside the model's reach.
func RenderPDF(t *Tailored, items []resume.Item, h Header) ([]byte, error) {
	const (
		marginL = 18.0
		marginR = 18.0
		marginT = 16.0
	)

	pdf := fpdf.New("P", "mm", "A4", "")

	// Core PDF fonts use cp1252, not UTF-8. tr converts our UTF-8 strings so
	// characters like "·" and "•" render correctly instead of as mojibake.
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	pdf.SetMargins(marginL, marginT, marginR)
	pdf.SetAutoPageBreak(true, 16)
	pdf.AddPage()

	pageW, _ := pdf.GetPageSize()
	textW := pageW - marginL - marginR

	// --- Header: name + contact line ---
	if h.Name != "" {
		pdf.SetFont("Helvetica", "B", 20)
		pdf.CellFormat(textW, 9, tr(h.Name), "", 1, "L", false, 0, "")
	}
	if cl := h.contactLine(); cl != "" {
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.SetTextColor(90, 90, 90)
		pdf.MultiCell(textW, 5, tr(cl), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}

	// --- Summary ---
	if t.Summary != "" {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "", 10.5)
		pdf.MultiCell(textW, 5, tr(t.Summary), "", "L", false)
	}

	g := groupByCompany(t, items)

	sectionHeading := func(title string) {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.CellFormat(textW, 7, tr(title), "", 1, "L", false, 0, "")
		y := pdf.GetY()
		pdf.SetDrawColor(180, 180, 180)
		pdf.Line(marginL, y, pageW-marginR, y)
		pdf.Ln(1.5)
	}

	// --- Professional Experience (company-grouped) ---
	if len(g.Experience) > 0 {
		sectionHeading("Professional Experience")
		for _, e := range g.Experience {
			pdf.Ln(1)
			// Company — Role on the left, date range right-aligned on the same row.
			pdf.SetFont("Helvetica", "B", 10.5)
			left := e.Company
			if e.Role != "" {
				left += " — " + e.Role
			}
			dr := dateRange(e.Start, e.End)
			pdf.CellFormat(textW*0.68, 6, tr(left), "", 0, "L", false, 0, "")
			pdf.SetFont("Helvetica", "I", 9)
			pdf.SetTextColor(90, 90, 90)
			pdf.CellFormat(textW*0.32, 6, tr(dr), "", 1, "R", false, 0, "")
			pdf.SetTextColor(0, 0, 0)

			pdf.SetFont("Helvetica", "", 10)
			for _, b := range e.Bullets {
				drawBullet(pdf, tr, marginL, textW, b)
			}
		}
	}

	// --- Other sections (skills, etc.) ---
	for _, s := range g.Other {
		sectionHeading(s.Heading)
		pdf.SetFont("Helvetica", "", 10)
		for _, b := range s.Bullets {
			drawBullet(pdf, tr, marginL, textW, b)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("tailor: render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// drawBullet renders one wrapped bullet with a hanging indent, so wrapped lines
// align under the text rather than under the "•".
func drawBullet(pdf *fpdf.Fpdf, tr func(string) string, marginL, textW float64, text string) {
	const (
		bulletW = 5.0
		lineH   = 5.0
	)
	x := marginL
	y := pdf.GetY()

	pdf.SetXY(x, y)
	pdf.CellFormat(bulletW, lineH, tr("\u2022"), "", 0, "L", false, 0, "")

	pdf.SetLeftMargin(x + bulletW)
	pdf.SetX(x + bulletW)
	pdf.MultiCell(textW-bulletW, lineH, tr(text), "", "L", false)
	pdf.SetLeftMargin(marginL)
}
