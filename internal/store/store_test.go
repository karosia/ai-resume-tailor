package store

import "testing"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProfile_SaveAndGet(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	// Empty by default — not an error.
	p, err := st.GetProfile()
	if err != nil {
		t.Fatalf("get empty profile: %v", err)
	}
	if p.Name != "" || p.GitHub != "" {
		t.Fatalf("expected empty profile, got %+v", p)
	}

	// Save then read back.
	want := Profile{
		Name: "J", Email: "j@example.com", Phone: "+1 604 555 0100",
		Location: "Canada", LinkedIn: "linkedin.com/in/j", GitHub: "github.com/j",
	}
	if err := st.SaveProfile(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.GetProfile()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}

	// Save again — should overwrite the single row, not insert a second.
	want.Name = "J"
	if err := st.SaveProfile(want); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, _ = st.GetProfile()
	if got.Name != "J" {
		t.Fatalf("update didn't take: %+v", got)
	}
}
