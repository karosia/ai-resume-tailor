package resume

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ReadResumeFile loads resume text from a .txt or .pdf file. For PDFs it
// extracts the plain text; if extraction yields too little usable text (a
// scanned image, an encrypted file, or an unusual layout), it returns an error
// suggesting the user paste the resume as plain text instead. A clear dead end
// with a hint beats silently feeding garbage to the LLM.
func ReadResumeFile(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return readPDF(path)
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// minExtractedChars is the safety-net threshold: fewer than this many characters
// out of a PDF almost always means extraction failed rather than that the resume
// is genuinely that short.
const minExtractedChars = 100

func readPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("could not extract text from %s (it may be scanned or encrypted); "+
			"paste the resume into a .txt file and pass that instead: %w", filepath.Base(path), err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", fmt.Errorf("read pdf text: %w", err)
	}

	text := strings.TrimSpace(buf.String())
	if len(text) < minExtractedChars {
		return "", fmt.Errorf("extracted only %d characters from %s — it may be scanned or use a "+
			"complex layout; paste the resume into a .txt file and pass that instead",
			len(text), filepath.Base(path))
	}
	return text, nil
}
