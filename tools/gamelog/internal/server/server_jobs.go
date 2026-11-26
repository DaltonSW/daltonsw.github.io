package server

import (
	"fmt"
	"sync"
	"time"
)

// job tracks one long-running background operation — the whole-library
// runs (refresh every game's achievements; Catch up's "Load what's new") slow
// enough (RA throttles to ~1.2s/call, and there can be 200+ linked games)
// that they can't run inside a single HTTP request. In-memory only: this is
// a single-user local tool, so a job doesn't need to survive a restart of
// the server.
//
// Title/BackHref/BackLabel describe the job's monitor page. They're set once
// in create, before the worker goroutine starts, and never mutated after —
// so handlers read them directly rather than through the mutex.
type job struct {
	mu          sync.Mutex
	Status      string // "running" | "done" | "error"
	Lines       []logLine
	Err         string
	Current     int    // 1-based index of the item currently in flight (== Total when done)
	Total       int    // number of items this job will process
	CurrentItem string // title of the item currently in flight; "" once finished

	Title     string // heading for the job monitor page
	BackHref  string // where the monitor's "back" link points
	BackLabel string // that link's text
	Nav       string // which nav item the monitor highlights

	// DoneHref, when set, is where the monitor sends the browser once the job
	// finishes cleanly (an HX-Redirect on the progress poll) — Catch up's run
	// lands straight back on the queue it just built rather than on its log.
	DoneHref string
}

// logLine is one line of a job's log. Href, when set, makes the line a link.
type logLine struct {
	Text string
	Href string
}

func (j *job) log(format string, args ...any) {
	j.logLink("", format, args...)
}

func (j *job) logLink(href, format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Lines = append(j.Lines, logLine{Text: fmt.Sprintf(format, args...), Href: href})
}

// progress records structured position (for a progress bar), independent of
// the free-text log lines log appends.
func (j *job) progress(current, total int, item string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Current, j.Total, j.CurrentItem = current, total, item
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
func (j *job) snapshot() (status string, lines []logLine, errMsg string, current, total int, currentItem string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, append([]logLine(nil), j.Lines...), j.Err, j.Current, j.Total, j.CurrentItem
}

type jobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*job
}

func newJobRegistry() *jobRegistry {
	return &jobRegistry{jobs: map[string]*job{}}
}

func (r *jobRegistry) create(title, backHref, backLabel string) (string, *job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	j := &job{Status: "running", Title: title, BackHref: backHref, BackLabel: backLabel, Nav: "housekeeping"}
	r.jobs[id] = j
	return id, j
}

// running reports whether the job with this id exists and hasn't finished.
func (r *jobRegistry) running(id string) bool {
	j := r.get(id)
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status == "running"
}

func (r *jobRegistry) get(id string) *job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}
