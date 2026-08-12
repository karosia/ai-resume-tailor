package resume

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
)

// makePDF writes a one-page PDF containing body at dir/name and returns its path.
func makePDF(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := fpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Helvetica", "", 12)
	p.MultiCell(0, 6, body, "", "L", false)
	var out bytes.Buffer
	if err := p.Output(&out); err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	return path
}

func TestReadResumeFile_TXT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.txt")
	os.WriteFile(path, []byte("plain text resume content"), 0o644)

	got, err := ReadResumeFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain text resume content" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestReadResumeFile_PDF(t *testing.T) {
	body := "Senior Backend Engineer specializing in Go and distributed systems. " +
		"Raised uptime from 99.2% to 99.98% at Northwind Logistics over three years."
	path := makePDF(t, t.TempDir(), "resume.pdf", body)

	got, err := ReadResumeFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Extraction can drop the odd space, so check for distinctive substrings
	// rather than exact equality.
	if !strings.Contains(got, "Backend Engineer") || !strings.Contains(got, "Northwind") {
		t.Fatalf("extracted text missing expected content: %q", got)
	}
}

func TestReadResumeFile_PDFTooShortRejected(t *testing.T) {
	path := makePDF(t, t.TempDir(), "tiny.pdf", "Hi")

	_, err := ReadResumeFile(path)
	if err == nil {
		t.Fatal("expected an error for a PDF with too little extractable text")
	}
	if !strings.Contains(err.Error(), ".txt") {
		t.Fatalf("error should hint at pasting as .txt, got: %v", err)
	}
}
