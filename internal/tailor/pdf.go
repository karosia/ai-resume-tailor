package tailor

import (
	"bytes"
	"fmt"

	"github.com/go-pdf/fpdf"
)

// PDFOptions controls the header of the rendered resume. These are facts about
// the candidate (name, contact line) that live outside the tailored items, so
// the caller supplies them explicitly rather than the model inventing them.
type PDFOptions struct {
	Name    string // e.g. "Jayce Park"
	Contact string // e.g. "Vancouver, BC · you@example.com · github.com/you"
}

// RenderPDF lays out a tailored resume as a single-column A4 PDF and returns the
// file bytes. Layout constants are chosen for a clean, ATS-friendly document:
// standard fonts, generous margins, no columns or graphics that parsers choke on.
func RenderPDF(t *Tailored, opts PDFOptions) ([]byte, error) {
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

	// --- Header ---
	if opts.Name != "" {
		pdf.SetFont("Helvetica", "B", 20)
		pdf.CellFormat(textW, 9, tr(opts.Name), "", 1, "L", false, 0, "")
	}
	if opts.Contact != "" {
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.SetTextColor(90, 90, 90)
		pdf.MultiCell(textW, 5, tr(opts.Contact), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}

	// --- Summary ---
	if t.Summary != "" {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "", 10.5)
		pdf.MultiCell(textW, 5, tr(t.Summary), "", "L", false)
	}

	// --- Sections ---
	for _, s := range t.Sections {
		pdf.Ln(3)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.CellFormat(textW, 7, tr(s.Heading), "", 1, "L", false, 0, "")
		y := pdf.GetY()
		pdf.SetDrawColor(180, 180, 180)
		pdf.Line(marginL, y, pageW-marginR, y)
		pdf.Ln(1.5)

		pdf.SetFont("Helvetica", "", 10)
		for _, b := range s.Bullets {
			drawBullet(pdf, tr, marginL, textW, b.Text)
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
