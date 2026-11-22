package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The refresh-all monitor is layout-only Go: the log's structure is recovered
// from line indentation at render time (logLineClass) and the page relies on
// the body--fixed class to stop the window scrolling. Both are invisible to
// every other test, so render the page and check them.
func TestJobPageRenders(t *testing.T) {
	tmpl, err := parsePage(pageSpecs["job"].name, pageSpecs["job"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	data := jobData{
		Page:    Page{Title: "Achievements — refresh all", Nav: "housekeeping", BodyClass: "body--fixed"},
		ID:      "123",
		Status:  "running",
		Current: 3, Total: 12, CurrentItem: "Banjo-Kazooie",
		Lines: []logLine{
			{Text: "Banjo-Kazooie", Href: "/games/banjo-kazooie?suggest=1#suggest"},
			{Text: "  retroachievements: 12/189 unlocked, 2026-01-01 to 2026-01-02"},
			{Text: ""},
		},
		Heading:   "Achievements — refresh all",
		BackHref:  "/housekeeping",
		BackLabel: "Back to housekeeping",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`<body class="body--fixed">`,
		`hx-get="/jobs/123/progress"`,
		`id="job-log"`,
		`class="job__line job__line--head"><a href="/games/banjo-kazooie?suggest=1#suggest">Banjo-Kazooie</a>`,
		`class="job__line job__line--sub">`,
		`class="job__line job__line--gap">`,
		`width: 25%`,
		"Banjo-Kazooie</span>", // the "fetching" stat
		`<h1>Achievements — refresh all</h1>`,
		`href="/housekeeping">Back to housekeeping</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("job page missing %q", want)
		}
	}

	// A finished job must stop polling, or the page keeps hammering the
	// server for a snapshot that can no longer change.
	data.Status, data.CurrentItem, data.Current = "done", "", 12
	buf.Reset()
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "hx-trigger") {
		t.Error("finished job still polls")
	}
	if !strings.Contains(buf.String(), "job__stat-value--done") {
		t.Error("finished job not reported as done")
	}
}

// Catch up's run sends the browser back to the queue once it's done, via the
// progress poll — but only on a clean finish, so a failed run stays on its log.
func TestJobProgress_RedirectsWhenDone(t *testing.T) {
	s := &server{jobs: newJobRegistry()}
	poll := func(id string) string {
		req := httptest.NewRequest(http.MethodGet, "/jobs/"+id+"/progress", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		s.handleJobProgress(rec, req)
		return rec.Header().Get("HX-Redirect")
	}

	id, j := s.jobs.create("Catch up", "/", "Back")
	j.DoneHref = "/?min_hours=5"
	if got := poll(id); got != "" {
		t.Errorf("running job redirected to %q", got)
	}
	j.finish(nil)
	if got := poll(id); got != "/?min_hours=5" {
		t.Errorf("done job redirect = %q", got)
	}

	failed, fj := s.jobs.create("Catch up", "/", "Back")
	fj.DoneHref = "/"
	fj.finish(errors.New("boom"))
	if got := poll(failed); got != "" {
		t.Errorf("failed job redirected to %q; it should stay on its log", got)
	}

	plain, pj := s.jobs.create("Refresh all", "/housekeeping", "Back")
	pj.finish(nil)
	if got := poll(plain); got != "" {
		t.Errorf("a job without DoneHref redirected to %q", got)
	}
}
