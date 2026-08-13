package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"ai-resume-tailor/internal/store"
)

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv, err := NewServer(st, nil)
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

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
