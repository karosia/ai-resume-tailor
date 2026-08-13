package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-resume-tailor/internal/store"
)

// fakeRunner stands in for the real LLM-backed runner. tailorErr/prepErr let a
// test force a failure; otherwise the funcs return a canned result immediately.
type fakeRunner struct {
	result string
}

func (f fakeRunner) Tailor(ctx context.Context, jd string) (string, error) { return f.result, nil }
func (f fakeRunner) Prep(ctx context.Context, jd string) (string, error)   { return f.result, nil }

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv, err := NewServer(st, fakeRunner{result: "RENDERED OUTPUT"}, nil)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, srv.Handler()
}

func TestDashboard_EmptyState(t *testing.T) {
	_, h := newTestServer(t)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No applications yet") {
		t.Fatal("empty state message missing")
	}
}

func TestAdd_ThenListed(t *testing.T) {
	srv, h := newTestServer(t)

	form := url.Values{"company": {"Shopify"}, "role": {"Senior Backend Engineer"}}
	req := httptest.NewRequest("POST", "/apps", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// POST-redirect-GET: expect a 303 See Other.
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}

	// It should now be persisted and rendered on the dashboard.
	apps, _ := srv.store.List()
	if len(apps) != 1 || apps[0].Company != "Shopify" {
		t.Fatalf("application not stored: %+v", apps)
	}

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rr2.Body.String(), "Shopify") {
		t.Fatal("added application not shown on dashboard")
	}
}

func TestAdd_MissingFieldsRejected(t *testing.T) {
	_, h := newTestServer(t)

	form := url.Values{"company": {""}, "role": {"Engineer"}}
	req := httptest.NewRequest("POST", "/apps", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing company, got %d", rr.Code)
	}
}

func TestSetStatus_UpdatesAndStamps(t *testing.T) {
	srv, h := newTestServer(t)
	app, _ := srv.store.Add("Stripe", "Backend Engineer")

	form := url.Values{"status": {"applied"}}
	req := httptest.NewRequest("POST", "/apps/"+itoa(app.ID)+"/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	got, _ := srv.store.Get(app.ID)
	if got.Status != store.StatusApplied || got.AppliedAt == nil {
		t.Fatalf("status/applied_at not updated: %+v", got)
	}
}

func TestSetStatus_BadIDRejected(t *testing.T) {
	_, h := newTestServer(t)

	req := httptest.NewRequest("POST", "/apps/notanumber/status", strings.NewReader("status=applied"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric id, got %d", rr.Code)
	}
}

func TestGeneratePage_Renders(t *testing.T) {
	_, h := newTestServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/generate", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Tailor") {
		t.Fatal("generate page missing tailor action")
	}
}

func TestGenerateSubmit_CreatesJobAndCompletes(t *testing.T) {
	_, h := newTestServer(t)

	form := url.Values{"jd": {"Senior Go Engineer\nBuild distributed systems"}, "action": {"tailor"}}
	req := httptest.NewRequest("POST", "/generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// Should redirect to the new job's page.
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/jobs/") {
		t.Fatalf("expected redirect to /jobs/..., got %q", loc)
	}

	// Poll the job page until the background job finishes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jr := httptest.NewRecorder()
		h.ServeHTTP(jr, httptest.NewRequest("GET", loc, nil))
		if jr.Code != http.StatusOK {
			t.Fatalf("job page status %d", jr.Code)
		}
		if strings.Contains(jr.Body.String(), "RENDERED OUTPUT") {
			return // done — result rendered
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not complete / render its result in time")
}

func TestGenerateSubmit_EmptyJDRejected(t *testing.T) {
	_, h := newTestServer(t)
	form := url.Values{"jd": {"   "}, "action": {"tailor"}}
	req := httptest.NewRequest("POST", "/generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty JD, got %d", rr.Code)
	}
}

func TestJobPage_UnknownIDNotFound(t *testing.T) {
	_, h := newTestServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/jobs/deadbeef", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
