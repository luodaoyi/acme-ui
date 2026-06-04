package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/acme-ui/acme-ui/internal/auth"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type LogEntry struct {
	At     time.Time `json:"at"`
	Stream string    `json:"stream"`
	Text   string    `json:"text"`
}

type Snapshot struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     Status     `json:"status"`
	Command    string     `json:"command"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	Error      string     `json:"error,omitempty"`
	Logs       []LogEntry `json:"logs,omitempty"`
}

type Event struct {
	Type string    `json:"type"`
	Log  *LogEntry `json:"log,omitempty"`
	Job  *Snapshot `json:"job,omitempty"`
}

type Job struct {
	id          string
	kind        string
	status      Status
	command     string
	startedAt   time.Time
	finishedAt  *time.Time
	exitCode    *int
	err         string
	logs        []LogEntry
	cancel      context.CancelFunc
	subscribers map[chan Event]struct{}
}

type Store struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewStore() *Store {
	return &Store{jobs: make(map[string]*Job)}
}

func (s *Store) Start(kind, command string, run func(ctx context.Context, emit func(stream, text string)) (int, error)) (*Snapshot, error) {
	id, err := auth.GenerateToken(12)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{
		id:          id,
		kind:        kind,
		status:      StatusRunning,
		command:     command,
		startedAt:   time.Now(),
		cancel:      cancel,
		subscribers: make(map[chan Event]struct{}),
	}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()

	go func() {
		exitCode, err := run(ctx, func(stream, text string) {
			s.appendLog(id, stream, text)
		})
		s.finish(id, exitCode, err, ctx.Err() == context.Canceled)
	}()

	return s.snapshot(job, false), nil
}

func (s *Store) Get(id string, includeLogs bool) (*Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	return s.snapshot(job, includeLogs), true
}

func (s *Store) List() []*Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Snapshot, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, s.snapshot(job, false))
	}
	return out
}

func (s *Store) Cancel(id string) bool {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if ok && job.status == StatusRunning {
		job.cancel()
	}
	s.mu.Unlock()
	return ok
}

func (s *Store) CancelAll() {
	s.mu.Lock()
	var cancels []context.CancelFunc
	for _, job := range s.jobs {
		if job.status == StatusRunning {
			cancels = append(cancels, job.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Store) Subscribe(id string) (<-chan Event, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, nil, false
	}
	ch := make(chan Event, 64)
	job.subscribers[ch] = struct{}{}
	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if job, ok := s.jobs[id]; ok {
			delete(job.subscribers, ch)
		}
		close(ch)
	}
	return ch, unsubscribe, true
}

func (s *Store) appendLog(id, stream, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	entry := LogEntry{At: time.Now(), Stream: stream, Text: text}
	job.logs = append(job.logs, entry)
	s.broadcastLocked(job, Event{Type: "log", Log: &entry})
}

func (s *Store) finish(id string, exitCode int, err error, canceled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	now := time.Now()
	job.finishedAt = &now
	job.exitCode = &exitCode
	switch {
	case canceled:
		job.status = StatusCanceled
		job.err = "canceled"
	case err != nil:
		job.status = StatusFailed
		job.err = err.Error()
	default:
		job.status = StatusSucceeded
	}
	snapshot := s.snapshot(job, false)
	s.broadcastLocked(job, Event{Type: "state", Job: snapshot})
}

func (s *Store) broadcastLocked(job *Job, event Event) {
	for ch := range job.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Store) snapshot(job *Job, includeLogs bool) *Snapshot {
	snapshot := &Snapshot{
		ID:         job.id,
		Kind:       job.kind,
		Status:     job.status,
		Command:    job.command,
		StartedAt:  job.startedAt,
		FinishedAt: job.finishedAt,
		ExitCode:   job.exitCode,
		Error:      job.err,
	}
	if includeLogs {
		snapshot.Logs = append([]LogEntry(nil), job.logs...)
	}
	return snapshot
}
