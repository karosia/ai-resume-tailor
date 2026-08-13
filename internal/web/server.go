// Package web serves a local dashboard for the application tracker and a page for
// running the slower LLM operations (tailoring, interview prep) as background
// jobs. It renders server-side with html/template and reuses the store package
// directly — no JavaScript framework and no API layer.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-resume-tailor/internal/jobs"
	"ai-resume-tailor/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Runner executes the slow, LLM-backed operations. The web package depends only
// on this interface, so it stays decoupled from the llm/tailor/prep packages;
// the cli wires up the real implementation.
type Runner interface {
	Tailor(ctx context.Context, jdText string) (string, error)
	Prep(ctx context.Context, jdText string) (string, error)
}

type Server struct {
	store  *store.Store
	runner Runner
	jobs   *jobs.Manager
	tmpl   *template.Template
	log    *slog.Logger
}

func NewServer(st *store.Store, runner Runner, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse templates: %w", err)
	}
	return &Server{
		store:  st,
		runner: runner,
		jobs:   jobs.NewManager(3 * time.Minute), // generous ceiling for tailor+prep
		tmpl:   tmpl,
		log:    log,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("POST /apps", s.add)
	mux.HandleFunc("POST /apps/{id}/status", s.setStatus)
	mux.HandleFunc("POST /apps/{id}/note", s.setNote)
	mux.HandleFunc("GET /generate", s.generatePage)
	mux.HandleFunc("POST /generate", s.generateSubmit)
	mux.HandleFunc("GET /jobs/{id}", s.jobPage)
	return mux
}

// page wraps template data with the active nav tab, so the shared nav partial
// can highlight the current section.
type page struct {
	Active string
	Data   any
}

func (s *Server) render(w http.ResponseWriter, name, active string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, page{Active: active, Data: data}); err != nil {
		s.serverError(w, "render "+name, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// --- tracker (unchanged behavior) ---

type appView struct {
	ID      int64
	Company string
	Role    string
	Status  store.Status
	Notes   string
	Applied string
	Updated string
}

type dashboardData struct {
	Apps     []appView
	Statuses []store.Status
	Pipeline []store.Status
}

func toView(a store.Application) appView {
	applied := "—"
	if a.AppliedAt != nil {
		applied = a.AppliedAt.Local().Format("2006-01-02")
	}
	return appView{
		ID: a.ID, Company: a.Company, Role: a.Role, Status: a.Status, Notes: a.Notes,
		Applied: applied,
		Updated: a.UpdatedAt.Local().Format("2006-01-02 15:04"),
	}
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.List()
	if err != nil {
		s.serverError(w, "load applications", err)
		return
	}
	views := make([]appView, 0, len(apps))
	for _, a := range apps {
		views = append(views, toView(a))
	}
	s.render(w, "dashboard.html", "pipeline", dashboardData{
		Apps:     views,
		Statuses: store.AllStatuses(),
		Pipeline: []store.Status{
			store.StatusDraft, store.StatusApplied, store.StatusInterviewing,
			store.StatusOffer, store.StatusAccepted,
		},
	})
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	company := strings.TrimSpace(r.FormValue("company"))
	role := strings.TrimSpace(r.FormValue("role"))
	if company == "" || role == "" {
		http.Error(w, "company and role are required", http.StatusBadRequest)
		return
	}
	if _, err := s.store.Add(company, role); err != nil {
		s.serverError(w, "add application", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetStatus(id, store.Status(r.FormValue("status"))); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) setNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetNotes(id, strings.TrimSpace(r.FormValue("note"))); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "save note", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- generate (background jobs) ---

type generateData struct {
	Jobs []jobs.Job
}

func (s *Server) generatePage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "generate.html", "generate", generateData{Jobs: s.jobs.List()})
}

func (s *Server) generateSubmit(w http.ResponseWriter, r *http.Request) {
	jd := strings.TrimSpace(r.FormValue("jd"))
	action := r.FormValue("action")
	if jd == "" {
		http.Error(w, "a job description is required", http.StatusBadRequest)
		return
	}

	var fn func(ctx context.Context) (string, error)
	switch action {
	case "tailor":
		fn = func(ctx context.Context) (string, error) { return s.runner.Tailor(ctx, jd) }
	case "prep":
		fn = func(ctx context.Context) (string, error) { return s.runner.Prep(ctx, jd) }
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	// Kick off the slow work in the background and hand the user a job page to
	// watch. The handler returns in milliseconds.
	id := s.jobs.Submit(action, label(jd), fn)
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

func (s *Server) jobPage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, "job.html", "generate", job)
}

// label is a short, single-line summary of a JD for the jobs list.
func label(jd string) string {
	line := jd
	if i := strings.IndexByte(line, '\n'); i != -1 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if len(line) > 70 {
		line = line[:67] + "…"
	}
	if line == "" {
		line = "job description"
	}
	return line
}

// --- helpers ---

func (s *Server) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid application id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func (s *Server) serverError(w http.ResponseWriter, what string, err error) {
	s.log.Error("web: "+what, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
