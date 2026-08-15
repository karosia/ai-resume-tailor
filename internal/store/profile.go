package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// Profile holds the candidate's contact facts shown at the top of a tailored
// resume. It's stored as a single row (id = 1); there is only ever one profile.
type Profile struct {
	Name     string
	Email    string
	Phone    string
	Location string
	LinkedIn string
	GitHub   string
}

// GetProfile returns the saved profile. If none has been saved yet, it returns
// a zero-value Profile (all fields empty) and no error — an empty profile is a
// valid state, not a "not found" condition.
func (s *Store) GetProfile() (Profile, error) {
	const q = `SELECT name, email, phone, location, linkedin, github
	           FROM profile WHERE id = 1`
	var p Profile
	err := s.db.QueryRow(q).Scan(&p.Name, &p.Email, &p.Phone, &p.Location, &p.LinkedIn, &p.GitHub)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("store: get profile: %w", err)
	}
	return p, nil
}

// SaveProfile writes the profile, creating the single row on first save and
// overwriting it thereafter (upsert on the fixed id = 1).
func (s *Store) SaveProfile(p Profile) error {
	const q = `
	INSERT INTO profile (id, name, email, phone, location, linkedin, github)
	VALUES (1, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
	    name = excluded.name,
	    email = excluded.email,
	    phone = excluded.phone,
	    location = excluded.location,
	    linkedin = excluded.linkedin,
	    github = excluded.github`
	if _, err := s.db.Exec(q, p.Name, p.Email, p.Phone, p.Location, p.LinkedIn, p.GitHub); err != nil {
		return fmt.Errorf("store: save profile: %w", err)
	}
	return nil
}
