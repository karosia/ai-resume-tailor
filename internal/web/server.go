// Package web serves a local dashboard for the application tracker. It renders
// server-side with html/template and reuses the store package directly — no
// JavaScript framework, no API layer, no external dependencies.
package web

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"jobtailor/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Server holds the dependencies the handlers need.
type Server struct {
	store *store.Store
	tmpl  *template.Template
	log   *slog.Logger
}

// NewServer parses the embedded templates and wires the store.
func NewServer(st *store.Store, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse templates: %w", err)
	}
	return &Server{store: st, tmpl: tmpl, log: log}, nil
}

// Handler builds the router. Go 1.22+ patterns carry the method and path
// variables, so no third-party router is needed.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard) // exactly "/"
	mux.HandleFunc("POST /apps", s.add)
	mux.HandleFunc("POST /apps/{id}/status", s.setStatus)
	mux.HandleFunc("POST /apps/{id}/note", s.setNote)
	return mux
}

// --- view model: pre-formatted for the template ---

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
	Statuses []store.Status // all statuses, for the change dropdown
	Pipeline []store.Status // the ordered progression, for the legend rail
}

func toView(a store.Application) appView {
	applied := "—"
	if a.AppliedAt != nil {
		applied = a.AppliedAt.Local().Format("2006-01-02")
	}
	return appView{
		ID:      a.ID,
		Company: a.Company,
		Role:    a.Role,
		Status:  a.Status,
		Notes:   a.Notes,
		Applied: applied,
		Updated: a.UpdatedAt.Local().Format("2006-01-02 15:04"),
	}
}

// --- handlers ---

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

	data := dashboardData{
		Apps:     views,
		Statuses: store.AllStatuses(),
		Pipeline: []store.Status{
			store.StatusDraft, store.StatusApplied, store.StatusInterviewing,
			store.StatusOffer, store.StatusAccepted,
		},
	}

	// Render to a buffer first: if the template errors we can still send a
	// clean 500 instead of a half-written page.
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "dashboard.html", data); err != nil {
		s.serverError(w, "render dashboard", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
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
	// POST-redirect-GET: reload as a fresh GET so a refresh won't resubmit.
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

// pathID parses the {id} path value, writing a 400 and returning false on error.
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
