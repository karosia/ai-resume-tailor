// Package jobs runs long-lived work — LLM calls that take a minute or two — in
// the background, so an HTTP handler can return immediately with a job id and the
// client can poll for the result. Jobs are kept in memory: this is a personal,
// single-user tool, and results don't need to survive a restart.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Job is a unit of background work and its outcome. Callers only ever receive
// copies (see Get/List), so they can read fields without racing the goroutine
// that fills them in.
type Job struct {
	ID      string
	Kind    string // "tailor" | "prep"
	Label   string // short human label, e.g. a snippet of the JD
	Status  Status
	Result  string // rendered output, when done
	PDF     []byte // tailor only: PDF bytes for download, when done
	PDFName string // tailor only: suggested download file name
	Err     string // error message, when failed
	Created time.Time
	Ended   time.Time
}

// Manager owns the set of jobs and the goroutines running them. A single mutex
// guards the map and every job's mutable fields — simple and correct for this
// scale, where contention is effectively nil.
type Manager struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	timeout time.Duration
}

func NewManager(timeout time.Duration) *Manager {
	return &Manager{jobs: make(map[string]*Job), timeout: timeout}
}

// JobResult is what a job's work function returns: the rendered text plus, for
// tailor jobs, the PDF bytes and file name to offer for download. Prep jobs
// leave PDF/PDFName empty.
type JobResult struct {
	Text    string
	PDF     []byte
	PDFName string
}

// Submit starts fn in the background and returns the new job's id immediately.
// fn receives a context that is cancelled when the manager's timeout elapses.
func (m *Manager) Submit(kind, label string, fn func(ctx context.Context) (JobResult, error)) string {
	job := &Job{
		ID:      newID(),
		Kind:    kind,
		Label:   label,
		Status:  StatusRunning,
		Created: time.Now(),
	}

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()

	go m.run(job, fn)
	return job.ID
}

func (m *Manager) run(job *Job, fn func(ctx context.Context) (JobResult, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	result, err := fn(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	job.Ended = time.Now()
	if err != nil {
		job.Status = StatusFailed
		job.Err = err.Error()
		return
	}
	job.Status = StatusDone
	job.Result = result.Text
	job.PDF = result.PDF
	job.PDFName = result.PDFName
}

// Get returns a copy of the job with the given id. Copying under the lock means
// the caller reads a consistent snapshot and never touches the live pointer.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// List returns a snapshot of all jobs, newest first.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, *j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
