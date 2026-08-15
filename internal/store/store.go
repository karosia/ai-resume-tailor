package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // ← 네 로컬은 이거 (샌드박스는 go-sqlite3)
)

// driverName is the name the SQLite driver registers with database/sql.
const driverName = "sqlite" // ← 네 로컬은 "sqlite" (샌드박스는 "sqlite3")

// tsLayout is how timestamps are stored: RFC3339 text. Storing times as explicit
// TEXT (rather than relying on driver-specific datetime handling) keeps behavior
// identical across drivers and makes the DB easy to inspect by hand.
const tsLayout = time.RFC3339Nano

// ErrNotFound is returned when an application id doesn't exist.
var ErrNotFound = errors.New("store: application not found")

// Store is a handle to the applications database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and ensures the
// schema exists. Pass ":memory:" for an ephemeral in-memory database (used in
// tests).
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

// Close releases the database handle.
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
	// Add columns that may be missing from an older database. CREATE TABLE
	// IF NOT EXISTS won't add columns to a table that already exists, so we
	// patch them in explicitly and ignore the "duplicate column" error that
	// means the column is already there.
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

// AddDraft creates a draft application that also carries the analyzed job
// description. It's what `tailor` calls after building a resume, so the JD is
// captured and the application is queued in the tracker for the user to act on.
// role or jdTitle may be empty; company is required.
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

// Add inserts a new application in the "draft" state and returns it.
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

// Get returns one application by id, or ErrNotFound.
func (s *Store) Get(id int64) (*Application, error) {
	row := s.db.QueryRow(
		`SELECT id, company, role, status, notes, jd_text, jd_title, applied_at, created_at, updated_at
		 FROM applications WHERE id = ?`, id)
	return scanApplication(row)
}

// List returns all applications, most recently updated first.
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

// SetStatus updates an application's status. The first time it becomes
// "applied", applied_at is stamped with the current time.
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

// SetNotes replaces an application's notes.
func (s *Store) SetNotes(id int64, notes string) error {
	res, err := s.db.Exec(
		`UPDATE applications SET notes = ?, updated_at = ? WHERE id = ?`,
		notes, time.Now().UTC().Format(tsLayout), id)
	if err != nil {
		return fmt.Errorf("store: update notes: %w", err)
	}
	return affected(res)
}

// Delete removes an application by id, or returns ErrNotFound if it doesn't
// exist. This is permanent — the row and its stored JD are gone.
func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM applications WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete application: %w", err)
	}
	return affected(res)
}

// affected turns "0 rows changed" into ErrNotFound, so callers can tell a
// missing id apart from a successful update.
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

// rowScanner is satisfied by both *sql.Row and *sql.Rows, so scanApplication
// works for single-row Get and multi-row List alike.
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
