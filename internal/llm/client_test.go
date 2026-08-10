package llm

import (
	"context"
	"errors"
	"testing"
)

type mockProvider struct {
	name string
	resp *Response
	err  error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Complete(ctx context.Context, req Request) (resp *Response, err error) {
	return m.resp, m.err
}

func TestClient_FallsBackToSecondProvider(t *testing.T) {
	primary := &mockProvider{name: "primary", err: errors.New("primary err")}
	backup := &mockProvider{name: "backup", resp: &Response{Content: "hi", Provider: "backup"}}

	c, err := NewClient(nil, primary, backup)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatalf("expected success via fallback, got error: %v", err)
	}
	if got.Provider != "backup" {
		t.Fatalf("expected backup to answer, got %q", got.Provider)
	}
}

func TestClient_AllProvidersFail(t *testing.T) {
	p1 := &mockProvider{name: "p1", err: errors.New("fail1")}
	p2 := &mockProvider{name: "p2", err: errors.New("fail2")}
	c, _ := NewClient(nil, p1, p2)

	_, err := c.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected an error when all providers fail")
	}
	if !contains(err.Error(), "fail1") || !contains(err.Error(), "fail2") {
		t.Fatalf("joined error missing detail: %v", err)
	}
}

func TestClient_PrimarySucceedsNoFallback(t *testing.T) {
	primary := &mockProvider{name: "primary", resp: &Response{Content: "ok", Provider: "primary"}}
	backup := &mockProvider{name: "backup", err: errors.New("should not be called")}
	c, _ := NewClient(nil, primary, backup)

	got, err := c.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "primary" {
		t.Fatalf("expected primary to answer, got %q", got.Provider)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
