package store

import "time"

type Status string

const (
	StatusDraft        Status = "draft"
	StatusApplied      Status = "applied"
	StatusInterviewing Status = "interviewing"
	StatusOffer        Status = "offer"
	StatusAccepted     Status = "accepted"
	StatusRejected     Status = "rejected"
	StatusWithdrawn    Status = "withdrawn"
)

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusApplied, StatusInterviewing,
		StatusOffer, StatusAccepted, StatusRejected, StatusWithdrawn:
		return true
	default:
		return false
	}
}

func AllStatuses() []Status {
	return []Status{
		StatusDraft, StatusApplied, StatusInterviewing,
		StatusOffer, StatusAccepted, StatusRejected, StatusWithdrawn,
	}
}

type Application struct {
	ID        int64
	Company   string
	Role      string
	Status    Status
	Notes     string
	JDText    string
	JDTitle   string
	AppliedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
