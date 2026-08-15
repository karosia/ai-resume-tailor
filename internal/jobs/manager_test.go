package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

// waitDone polls until the job leaves the running state, or fails the test.
func waitDone(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := m.Get(id)
		if !ok {
			t.Fatalf("job %s disappeared", id)
		}
		if j.Status != StatusRunning {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish in time", id)
	return Job{}
}

func TestManager_JobSucceeds(t *testing.T) {
	m := NewManager(2 * time.Second)
	id := m.Submit("test", "label", func(ctx context.Context) (JobResult, error) {
		return JobResult{Text: "the result"}, nil
	})

	j := waitDone(t, m, id)
	if j.Status != StatusDone {
		t.Fatalf("expected done, got %s (%s)", j.Status, j.Err)
	}
	if j.Result != "the result" {
		t.Fatalf("wrong result: %q", j.Result)
	}
	if j.Ended.IsZero() {
		t.Fatal("Ended should be set")
	}
}

func TestManager_JobFails(t *testing.T) {
	m := NewManager(2 * time.Second)
	id := m.Submit("test", "label", func(ctx context.Context) (JobResult, error) {
		return JobResult{}, errors.New("boom")
	})

	j := waitDone(t, m, id)
	if j.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", j.Status)
	}
	if j.Err != "boom" {
		t.Fatalf("wrong error: %q", j.Err)
	}
}

func TestManager_Timeout(t *testing.T) {
	m := NewManager(30 * time.Millisecond)
	id := m.Submit("test", "label", func(ctx context.Context) (JobResult, error) {
		<-ctx.Done() // block until the manager's timeout cancels us
		return JobResult{}, ctx.Err()
	})

	j := waitDone(t, m, id)
	if j.Status != StatusFailed {
		t.Fatalf("expected failed on timeout, got %s", j.Status)
	}
}

func TestManager_GetMissing(t *testing.T) {
	m := NewManager(time.Second)
	if _, ok := m.Get("nope"); ok {
		t.Fatal("expected missing job to report ok=false")
	}
}

func TestManager_ListNewestFirst(t *testing.T) {
	m := NewManager(time.Second)
	id1 := m.Submit("a", "1", func(ctx context.Context) (JobResult, error) { return JobResult{Text: "x"}, nil })
	time.Sleep(5 * time.Millisecond)
	id2 := m.Submit("b", "2", func(ctx context.Context) (JobResult, error) { return JobResult{Text: "y"}, nil })

	waitDone(t, m, id1)
	waitDone(t, m, id2)

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(list))
	}
	if list[0].ID != id2 {
		t.Fatalf("expected newest (id2) first, got %s", list[0].ID)
	}
}
