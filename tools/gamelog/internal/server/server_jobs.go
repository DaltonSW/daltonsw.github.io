package server

import (
	"fmt"
	"sync"
	"time"
)

// job tracks one long-running background operation — currently only
// "refresh every game's achievements", the one operation slow enough (RA
// throttles to ~1.2s/call, and there can be 200+ linked games) that it can't
// run inside a single HTTP request. In-memory only: this is a single-user
// local tool, so a job doesn't need to survive a restart of `gamelog serve`.
type job struct {
	mu     sync.Mutex
	Status string // "running" | "done" | "error"
	Lines  []string
	Err    string
}

func (j *job) log(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Lines = append(j.Lines, fmt.Sprintf(format, args...))
}

func (j *job) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err != nil {
		j.Status = "error"
		j.Err = err.Error()
		return
	}
	j.Status = "done"
}

// snapshot copies out what a status page needs, so the HTTP handler never
// holds the lock while rendering a template.
func (j *job) snapshot() (status string, lines []string, errMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, append([]string(nil), j.Lines...), j.Err
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*job
}

func newJobRegistry() *jobRegistry {
	return &jobRegistry{jobs: map[string]*job{}}
}

func (r *jobRegistry) create() (string, *job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	j := &job{Status: "running"}
	r.jobs[id] = j
	return id, j
}

func (r *jobRegistry) get(id string) *job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}
