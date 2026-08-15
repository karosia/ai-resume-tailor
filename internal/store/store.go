package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

const tsLayout = time.RFC3339Nano

var ErrNotFound = errors.New("store: application not found")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}

	// SQLite allows only one writer at a time, and a ":memory:" database is
	// per-connection. Capping the pool at one connection sidesteps both issues:
	// no "database is locked" errors, and in-memory tests see a single shared DB.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("store: ping db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS applications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    company    TEXT NOT NULL,
    role       TEXT NOT NULL,
    status     TEXT NOT NULL,
    notes      TEXT NOT NULL DEFAULT '',
    jd_text    TEXT NOT NULL DEFAULT '',
    jd_title   TEXT NOT NULL DEFAULT '',
    applied_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS profile (
    id        INTEGER PRIMARY KEY CHECK (id = 1), -- single-row table
    name      TEXT NOT NULL DEFAULT '',
    email     TEXT NOT NULL DEFAULT '',
    phone     TEXT NOT NULL DEFAULT '',
    location  TEXT NOT NULL DEFAULT '',
    linkedin  TEXT NOT NULL DEFAULT '',
    github    TEXT NOT NULL DEFAULT ''
);`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	for _, col := range []string{
		`ALTER TABLE applications ADD COLUMN jd_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE applications ADD COLUMN jd_title TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("store: migrate columns: %w", err)
		}
	}
	return nil
}

func (s *Store) Add(company, role string) (*Application, error) {
	company = strings.TrimSpace(company)
	role = strings.TrimSpace(role)
	if company == "" || role == "" {
		return nil, fmt.Errorf("store: company and role are required")
	}

	now := time.Now().UTC().Format(tsLayout)
	res, err := s.db.Exec(
		`INSERT INTO applications (company, role, status, notes, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, ?)`,
		company, role, string(StatusDraft), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("store: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: last insert id: %w", err)
	}
	return s.Get(id)
}

func (s *Store) Get(id int64) (*Application, error) {
	row := s.db.QueryRow(
		`SELECT id, company, role, status, notes, jd_text, jd_title, applied_at, created_at, updated_at
		 FROM applications WHERE id = ?`, id)
	return scanApplication(row)
}

func (s *Store) List() ([]Application, error) {
	rows, err := s.db.Query(
		`SELECT id, company, role, status, notes, jd_text, jd_title, applied_at, created_at, updated_at
		 FROM applications ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()

	var out []Application
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *app)
	}
	return out, rows.Err()
}

func (s *Store) SetStatus(id int64, status Status) error {
	if !status.Valid() {
		return fmt.Errorf("store: invalid status %q", status)
	}
	now := time.Now().UTC().Format(tsLayout)

	res, err := s.db.Exec(
		`UPDATE applications
		 SET status = ?,
		     updated_at = ?,
		     applied_at = CASE
		         WHEN ? = 'applied' AND applied_at IS NULL THEN ?
		         ELSE applied_at
		     END
		 WHERE id = ?`,
		string(status), now, string(status), now, id,
	)
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	return affected(res)
}

func (s *Store) SetNotes(id int64, notes string) error {
	res, err := s.db.Exec(
		`UPDATE applications SET notes = ?, updated_at = ? WHERE id = ?`,
		notes, time.Now().UTC().Format(tsLayout), id)
	if err != nil {
		return fmt.Errorf("store: update notes: %w", err)
	}
	return affected(res)
}

func affected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApplication(r rowScanner) (*Application, error) {
	var (
		app       Application
		status    string
		appliedAt sql.NullString
		createdAt string
		updatedAt string
	)
	err := r.Scan(&app.ID, &app.Company, &app.Role, &status, &app.Notes,
		&app.JDText, &app.JDTitle, &appliedAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan: %w", err)
	}

	app.Status = Status(status)
	if t, err := time.Parse(tsLayout, createdAt); err == nil {
		app.CreatedAt = t
	}
	if t, err := time.Parse(tsLayout, updatedAt); err == nil {
		app.UpdatedAt = t
	}
	if appliedAt.Valid {
		if t, err := time.Parse(tsLayout, appliedAt.String); err == nil {
			app.AppliedAt = &t
		}
	}
	return &app, nil
}

// AddDraft creates a draft application that also carries the analyzed job
// description. It's what tailor calls after building a resume.
func (s *Store) AddDraft(company, role, jdText, jdTitle string) (*Application, error) {
	company = strings.TrimSpace(company)
	if company == "" {
		return nil, fmt.Errorf("store: company is required")
	}
	role = strings.TrimSpace(role)

	now := time.Now().UTC().Format(tsLayout)
	res, err := s.db.Exec(
		`INSERT INTO applications (company, role, status, notes, jd_text, jd_title, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, ?, ?, ?)`,
		company, role, string(StatusDraft), jdText, jdTitle, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("store: insert draft: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: last insert id: %w", err)
	}
	return s.Get(id)
}
