package resume

import (
	"ai-resume-tailor/internal/llm"
	"context"
	"strings"
	"testing"
)

type fakeCompleter struct {
	content string
	err     error
}

func (f *fakeCompleter) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.content, Provider: "fake-provider", Model: "fake-model"}, nil
}

func TestDecompose_ParsesItems(t *testing.T) {
	canned := "```json\n" + `{"items":[
      {"type":"experience","title":"Senior Backend Engineer","company":"Acme","content":"Built a real-time platform","skills":["Go","NATS"],"metrics":["99.99% uptime"],"start_date":"2021-03","end_date":"2023-01"},
      {"type":"achievement","title":"","content":"Cut p99 latency by 40%","metrics":["40%"]}
    ]}` + "\n```\nHope that helps!"

	d := NewDecomposer(&fakeCompleter{content: canned}, nil)
	items, err := d.Decompose(context.Background(), "some resume text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Company != "Acme" || items[0].Skills[0] != "Go" {
		t.Fatalf("first item parsed wrong: %+v", items[0])
	}
	if !strings.HasPrefix(items[0].ID, "exp-") {
		t.Fatalf("expected exp- prefix, got %q", items[0].ID)
	}
	if !strings.HasPrefix(items[1].ID, "ach-") {
		t.Fatalf("expected ach- prefix, got %q", items[1].ID)
	}
}

func TestDecompose_DropsInvalidAndEmpty(t *testing.T) {
	canned := `{"items":[
      {"type":"bogus","content":"has invalid type"},
      {"type":"skill","content":"   "},
      {"type":"skill","content":"Go, distributed systems"}
    ]}`

	d := NewDecomposer(&fakeCompleter{content: canned}, nil)
	items, err := d.Decompose(context.Background(), "text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only 1 valid item, got %d", len(items))
	}
	if items[0].Content != "Go, distributed systems" {
		t.Fatalf("wrong surviving item: %+v", items[0])
	}
}

func TestDecompose_IDsAreDeterministicAndDeduped(t *testing.T) {
	canned := `{"items":[
      {"type":"skill","content":"Go"},
      {"type":"skill","content":"Go"}
    ]}`

	d := NewDecomposer(&fakeCompleter{content: canned}, nil)
	items, _ := d.Decompose(context.Background(), "text")
	if len(items) != 1 {
		t.Fatalf("expected dedup to 1 item, got %d", len(items))
	}
	if got := makeID(ItemSkill, "Go"); got != items[0].ID {
		t.Fatalf("ID not deterministic: %q vs %q", got, items[0].ID)
	}
}

func TestDecompose_EmptyResumeRejected(t *testing.T) {
	d := NewDecomposer(&fakeCompleter{content: "{}"}, nil)
	if _, err := d.Decompose(context.Background(), "   "); err == nil {
		t.Fatal("expected error for empty resume text")
	}
}
