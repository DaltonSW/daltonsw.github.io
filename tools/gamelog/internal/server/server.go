package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/forms"
	"go.dalton.dog/gamelog/internal/model"
)

//go:embed web/templates/*.html
var templateFiles embed.FS

//go:embed web/static
var staticFiles embed.FS

// DefaultPort is the port `gamelog serve` binds when no --port is given, and
// what bare `gamelog` (no subcommand) serves on.
const DefaultPort = 8080

// devMode is true when internal/server/web/templates exists on disk relative
// to the working directory the process started in — true for
// `go run ./cmd/gamelog` from tools/gamelog, false for an installed binary
// run from elsewhere. When true, templates and static assets are read
// straight from disk and templates reparsed on every request, so editing
// web/templates/*.html or web/static/* takes effect on the next browser
// refresh with no server restart (and no re-embed) needed.
//
// The path is rooted at internal/server, not the working directory itself:
// go:embed above is anchored to this package's source directory, and devMode
// mirrors that so the two agree on where web/ lives regardless of which
// directory `go run`/the binary is invoked from.
var devMode bool

const devWebRoot = "internal/server"

var templatesFS fs.FS
var staticFS fs.FS

var pageTemplatesCache map[string]*template.Template
var rowTemplateCache *template.Template
var playthroughFragmentsCache *template.Template
var jobProgressCache *template.Template

func init() {
	if info, err := os.Stat(devWebRoot + "/web/templates"); err == nil && info.IsDir() {
		devMode = true
	}

	templatesFS = fs.FS(templateFiles)
	staticFS = fs.FS(staticFiles)
	if devMode {
		templatesFS = os.DirFS(devWebRoot)
		staticFS = os.DirFS(devWebRoot)
	}

	if !devMode {
		pageTemplatesCache = make(map[string]*template.Template, len(pageSpecs))
		for name, spec := range pageSpecs {
			pageTemplatesCache[name] = loadPage(spec.name, spec.partials...)
		}
		rowTemplateCache = template.Must(template.New("game_row.html").Funcs(funcMap).
			ParseFS(templatesFS, "web/templates/game_row.html"))
		playthroughFragmentsCache = template.Must(template.New("playthrough_fragments.html").Funcs(funcMap).
			ParseFS(templatesFS, "web/templates/playthrough_fragments.html"))
		jobProgressCache = template.Must(template.New("job_progress.html").Funcs(funcMap).
			ParseFS(templatesFS, "web/templates/job_progress.html"))
	}
}

// Run starts the local web UI: a same-process, no-build-step alternative
// to the huh-based TUI, reusing the same data/write functions (and therefore
// the same never-lose-data safety net — see README.md/KNOWN-ISSUES.md) that
// the TUI does. Binds to 127.0.0.1 only; this is a personal local tool, never
// meant to be reachable off the machine it runs on.
func Run(port int) error {
	gamesDir, err := model.FindGamesDir()
	if err != nil {
		return err
	}
	s := &server{gamesDir: gamesDir, jobs: newJobRegistry()}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Printf("gamelog serve — http://%s  (Ctrl+C to stop)\n", addr)
	if devMode {
		fmt.Println("dev mode: web/templates and web/static served live from disk — refresh to see edits")
	}
	return http.ListenAndServe(addr, mux)
}

// server holds the state every handler needs. gamesDir is resolved once at
// startup, same as the TUI resolves it once per `run()` call.
type server struct {
	gamesDir string
	jobs     *jobRegistry
}

func (s *server) registerRoutes(mux *http.ServeMux) {
	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		panic(err) // embedded at build time; a failure here is a packaging bug, not a runtime condition
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /games/new", s.handleNewGameForm)
	mux.HandleFunc("POST /games/new", s.handleCreateGame)
	mux.HandleFunc("GET /games/{slug}", s.handleGameDetail)
	mux.HandleFunc("POST /games/{slug}/delete", s.handleDeleteGame)
	mux.HandleFunc("POST /games/{slug}/info", s.handleEditInfo)
	mux.HandleFunc("POST /games/{slug}/providers", s.handleUpdateProviders)
	mux.HandleFunc("POST /games/{slug}/quickedit", s.handleQuickEditGame)
	mux.HandleFunc("POST /games/{slug}/undraft", s.handleUndraftGame)
	mux.HandleFunc("POST /games/{slug}/playthroughs", s.handleNewPlaythrough)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}", s.handleUpdatePlaythrough)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/split", s.handleSplitPlaythrough)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions", s.handleAddSession)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions/{j}", s.handleEditSession)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions/{j}/delete", s.handleDeleteSession)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions/{j}/move-up", s.handleMoveSessionUp)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions/{j}/move-down", s.handleMoveSessionDown)
	mux.HandleFunc("POST /games/{slug}/planned", s.handleAddPlanned)
	mux.HandleFunc("POST /games/{slug}/planned/{idx}/start", s.handleStartPlanned)
	mux.HandleFunc("POST /games/{slug}/planned/{idx}/edit", s.handleEditPlanned)
	mux.HandleFunc("POST /games/{slug}/achievements", s.handleRefreshAchievements)

	mux.HandleFunc("GET /housekeeping", s.handleHousekeeping)
	mux.HandleFunc("POST /scan", s.handleScanCreate)
	mux.HandleFunc("POST /backlog", s.handleBacklogCreate)
	mux.HandleFunc("POST /subset/attach", s.handleSubsetAttach)
	mux.HandleFunc("POST /subset/create-base", s.handleSubsetCreateBase)
	mux.HandleFunc("POST /ignore", s.handleIgnore)
	mux.HandleFunc("POST /unignore", s.handleUnignore)
	mux.HandleFunc("POST /stale/{slug}", s.handleStaleAction)
	mux.HandleFunc("POST /close", s.handleCloseSelected)
	mux.HandleFunc("POST /achievements/all", s.handleAchievementsAll)
	mux.HandleFunc("GET /jobs/{id}", s.handleJobStatus)
	mux.HandleFunc("GET /jobs/{id}/progress", s.handleJobProgress)

	mux.HandleFunc("GET /suggest", s.handleSuggestPicker)
	mux.HandleFunc("GET /suggest/{slug}", s.handleSuggestReport)

	mux.HandleFunc("POST /project", s.handleProject)
}

// ── Page scaffolding ──────────────────────────────────────────────────────

// Page is embedded in every template's data so layout.html has a stable set
// of fields regardless of which page is rendering.
type Page struct {
	Title string
	Nav   string
	Flash Flash
	// BodyClass goes on <body>. Empty for the ordinary document-shaped pages;
	// set to "body--fixed" by pages that fill the viewport and scroll inside
	// their own panes instead of growing the page (see the job monitor).
	BodyClass string
}

type Flash struct {
	OK    string
	Error string
}

func newPage(r *http.Request, title, nav string) Page {
	q := r.URL.Query()
	return Page{Title: title, Nav: nav, Flash: Flash{OK: q.Get("ok"), Error: q.Get("error")}}
}

var funcMap = template.FuncMap{
	"pillClass": func(status string) string {
		if status == "" {
			return "pill pill--backlog"
		}
		return "pill pill--" + status
	},
	"dash":         forms.OrDash,
	"joinLines":    func(ss []string) string { return strings.Join(ss, "\n") },
	"gameStatuses": func() []string { return forms.GameStatuses },
	"pthStatuses":  func() []string { return forms.PlaythroughStatuses },
	"staleQuick":   func() []string { return forms.StaleQuickStatuses },
	"add1":         func(i int) int { return i + 1 },
	"itoa":         strconv.Itoa,
	"percent": func(current, total int) int {
		if total <= 0 {
			return 0
		}
		if p := current * 100 / total; p <= 100 {
			return p
		}
		return 100
	},
	"formatHoursFunc": commands.FormatHours,
	// A job's log is a flat []string that the writers indent by two spaces
	// for a game's per-provider detail lines (see runAchievementsAllJob), so
	// the shape is recoverable here rather than needing a structured record
	// per line: un-indented lines are the game headings, indented ones its
	// results, and the blank line before the summary is a spacer.
	"logLineClass": func(line string) string {
		switch {
		case strings.TrimSpace(line) == "":
			return "job__line job__line--gap"
		case strings.HasPrefix(line, "  "):
			return "job__line job__line--sub"
		}
		return "job__line job__line--head"
	},
	// dateOnly trims an RFC3339 achievement-unlock timestamp down to its date
	// for compact sidebar display; the full timestamp stays available via the
	// element's title attribute for anyone who needs the time too.
	"dateOnly": func(s string) string {
		if len(s) >= 10 {
			return s[:10]
		}
		return s
	},
}

// loadPage parses layout.html together with one page-specific template file
// and any additional partials it references, in an isolated
// *template.Template each call — every page file defines a template named
// "content", and parsing them one at a time like this is what lets each page
// reuse that same name without colliding with any other page's. In dev mode
// this is called fresh per request (see pageTemplatesCache), so a parse error
// panicking here would take the whole server down on a bad edit; callers in
// the request path use parsePage instead and turn errors into a 500.
func loadPage(name string, partials ...string) *template.Template {
	tmpl, err := parsePage(name, partials...)
	if err != nil {
		panic(err)
	}
	return tmpl
}

func parsePage(name string, partials ...string) (*template.Template, error) {
	files := append([]string{"web/templates/layout.html", "web/templates/" + name}, partials...)
	return template.New("layout.html").Funcs(funcMap).ParseFS(templatesFS, files...)
}

// pageSpecs describes how to (re)build each named page template; pageTemplatesCache
// (built once at startup) is used unless devMode, in which case render reparses
// from pageSpecs on every request so template edits show up without a restart.
var pageSpecs = map[string]struct {
	name     string
	partials []string
}{
	"index":          {"index.html", []string{"web/templates/game_row.html"}},
	"game_new":       {"game_new.html", nil},
	"game_detail":    {"game_detail.html", []string{"web/templates/playthrough_fragments.html"}},
	"housekeeping":   {"housekeeping.html", nil},
	"suggest_picker": {"suggest_picker.html", nil},
	"suggest_report": {"suggest_report.html", nil},
	"job":            {"job.html", []string{"web/templates/job_progress.html"}},
}

// renderRow renders game_row.html on its own — the fragment
// handleQuickEditGame swaps into the index page via htmx, as opposed to a
// full "layout" page render.
func (s *server) renderRow(w http.ResponseWriter, view gameRowView) {
	tmpl := rowTemplateCache
	if devMode {
		var err error
		tmpl, err = template.New("game_row.html").Funcs(funcMap).ParseFS(templatesFS, "web/templates/game_row.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "game_row", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderPlaythroughFragment renders one named template out of
// playthrough_fragments.html on its own — the htmx swap target for a
// playthrough/session edit on the game-detail page, as opposed to the full
// page reload that page used before. One file holds four related fragment
// templates (playthrough_header, sessions_table, session_row,
// playthroughs_section, each calling into the others), so this is
// parameterized by name rather than one bespoke method per fragment.
func (s *server) renderPlaythroughFragment(w http.ResponseWriter, name string, data any) {
	tmpl := playthroughFragmentsCache
	if devMode {
		var err error
		tmpl, err = template.New("playthrough_fragments.html").Funcs(funcMap).
			ParseFS(templatesFS, "web/templates/playthrough_fragments.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderJobProgress renders job_progress.html's "job_progress" block on its
// own — the htmx poll target /jobs/{id}/progress swaps into the job status
// page, as opposed to the full "layout" page handleJobStatus renders.
func (s *server) renderJobProgress(w http.ResponseWriter, data jobData) {
	tmpl := jobProgressCache
	if devMode {
		var err error
		tmpl, err = template.New("job_progress.html").Funcs(funcMap).
			ParseFS(templatesFS, "web/templates/job_progress.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "job_progress", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) render(w http.ResponseWriter, name string, data any) {
	spec, ok := pageSpecs[name]
	if !ok {
		http.Error(w, "unknown template "+name, http.StatusInternalServerError)
		return
	}
	tmpl := pageTemplatesCache[name]
	if devMode {
		var err error
		tmpl, err = parsePage(spec.name, spec.partials...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// redirectOK/redirectErr are this UI's stand-in for the TUI's ConfirmWrite
// prompt and printed result: the submitted form is already the user's
// confirmation (see mutate.WriteEntry/WriteSplit/WriteFrontMatter), and
// the outcome is reported via a flash query param on the page it lands back
// on instead of a terminal line.
func redirectOK(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, withFlash(path, "ok", msg), http.StatusSeeOther)
}

func redirectErr(w http.ResponseWriter, r *http.Request, path string, err error) {
	http.Redirect(w, r, withFlash(path, "error", err.Error()), http.StatusSeeOther)
}

func withFlash(path, key, val string) string {
	u, err := url.Parse(path)
	if err != nil {
		return path
	}
	q := u.Query()
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.String()
}

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
