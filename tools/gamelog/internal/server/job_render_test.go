package server

import (
	"bytes"
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
		Lines: []string{"Banjo-Kazooie", "  retroachievements: 12/189 unlocked, 2026-01-01 to 2026-01-02", ""},
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
		`class="job__line job__line--head">Banjo-Kazooie`,
		`class="job__line job__line--sub">`,
		`class="job__line job__line--gap">`,
		`width: 25%`,
		"Banjo-Kazooie</span>", // the "fetching" stat
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
