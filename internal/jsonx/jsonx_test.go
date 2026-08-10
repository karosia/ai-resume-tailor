package jsonx

import (
	"encoding/json"
	"testing"
)

// TestExtract exercises the JSON-extraction heuristic against the messy ways a
// model might wrap its output. Table-driven: each case is a row of data, and
// one loop runs them all. wantOK marks whether the extracted string is expected
// to parse as JSON.
func TestExtract(t *testing.T) {
	const payload = `{"items":[{"type":"skill","content":"Go"}]}`

	cases := []struct {
		name        string
		input       string
		wantOK      bool
		wantContent string
	}{
		{name: "plain json, no wrapping", input: payload, wantOK: true, wantContent: "Go"},
		{name: "fence with json language tag", input: "```json\n" + payload + "\n```", wantOK: true, wantContent: "Go"},
		{name: "bare fence, no language tag", input: "```\n" + payload + "\n```", wantOK: true, wantContent: "Go"},
		{name: "uppercase JSON tag", input: "```JSON\n" + payload + "\n```", wantOK: true, wantContent: "Go"},
		{name: "fence with trailing prose after close", input: "```json\n" + payload + "\n```\nHope that helps!", wantOK: true, wantContent: "Go"},
		{name: "prose before and after, no fence", input: "Here's the extracted data:\n" + payload + "\nLet me know if you need changes.", wantOK: true, wantContent: "Go"},
		{name: "prose first, then fenced block", input: "Sure!\n```json\n" + payload + "\n```", wantOK: true, wantContent: "Go"},
		{name: "leading whitespace and blank lines before fence", input: "\n\n   ```json\n" + payload + "\n```", wantOK: true, wantContent: "Go"},
		{name: "not json at all -> returned unchanged, unparseable", input: "I could not parse that resume.", wantOK: false},
	}

	type result struct {
		Items []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"items"`
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Extract(tc.input)

			var out result
			err := json.Unmarshal([]byte(got), &out)

			if tc.wantOK {
				if err != nil {
					t.Fatalf("expected parseable JSON, got error: %v\nextracted: %q", err, got)
				}
				if len(out.Items) != 1 || out.Items[0].Content != tc.wantContent {
					t.Fatalf("wrong content after extraction: %+v", out.Items)
				}
			} else {
				if err == nil {
					t.Fatalf("expected non-JSON input to fail parsing, but it parsed: %q", got)
				}
			}
		})
	}
}
