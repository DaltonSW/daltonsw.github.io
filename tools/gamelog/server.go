package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

//go:embed web/templates/*.html
var templateFiles embed.FS

//go:embed web/static
var staticFiles embed.FS

const defaultServePort = 8080

// runServe starts the local web UI: a same-process, no-build-step alternative
// to the huh-based TUI, reusing the same data/write functions (and therefore
// the same never-lose-data safety net — see README.md/KNOWN-ISSUES.md) that
// the TUI does. Binds to 127.0.0.1 only; this is a personal local tool, never
// meant to be reachable off the machine it runs on.
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", defaultServePort, "port to listen on (127.0.0.1 only)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gamelog serve [flags]\n\nServes a local web UI over the same content/games tree the TUI edits.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}
	s := &server{gamesDir: gamesDir, jobs: newJobRegistry()}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("gamelog serve — http://%s  (Ctrl+C to stop)\n", addr)
	return http.ListenAndServe(addr, mux)
}

// server holds the state every handler needs. gamesDir is resolved once at
// startup, same as the TUI resolves it once per `run()` call.
type server struct {
	gamesDir string
	jobs     *jobRegistry
}

func (s *server) registerRoutes(mux *http.ServeMux) {
	staticSub, err := fs.Sub(staticFiles, "web/static")
	if err != nil {
		panic(err) // embedded at build time; a failure here is a packaging bug, not a runtime condition
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /games/new", s.handleNewGameForm)
	mux.HandleFunc("POST /games/new", s.handleCreateGame)
	mux.HandleFunc("GET /games/{slug}", s.handleGameDetail)
	mux.HandleFunc("POST /games/{slug}/info", s.handleEditInfo)
	mux.HandleFunc("POST /games/{slug}/playthroughs", s.handleNewPlaythrough)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}", s.handleUpdatePlaythrough)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/split", s.handleSplitPlaythrough)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions", s.handleAddSession)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions/{j}", s.handleEditSession)
	mux.HandleFunc("POST /games/{slug}/playthroughs/{idx}/sessions/{j}/delete", s.handleDeleteSession)
	mux.HandleFunc("POST /games/{slug}/planned", s.handleAddPlanned)
	mux.HandleFunc("POST /games/{slug}/planned/{idx}/start", s.handleStartPlanned)
	mux.HandleFunc("POST /games/{slug}/planned/{idx}/edit", s.handleEditPlanned)
	mux.HandleFunc("POST /games/{slug}/achievements", s.handleRefreshAchievements)

	mux.HandleFunc("GET /scan", s.handleScan)
	mux.HandleFunc("POST /scan", s.handleScanCreate)

	mux.HandleFunc("GET /review", s.handleReview)
	mux.HandleFunc("POST /review/{slug}", s.handleReviewAction)

	mux.HandleFunc("GET /stale", s.handleStale)
	mux.HandleFunc("POST /stale/{slug}", s.handleStaleAction)

	mux.HandleFunc("GET /close", s.handleClose)
	mux.HandleFunc("POST /close", s.handleCloseSelected)

	mux.HandleFunc("GET /suggest", s.handleSuggestPicker)
	mux.HandleFunc("GET /suggest/{slug}", s.handleSuggestReport)

	mux.HandleFunc("GET /achievements", s.handleAchievementsHome)
	mux.HandleFunc("POST /achievements/all", s.handleAchievementsAll)
	mux.HandleFunc("GET /jobs/{id}", s.handleJobStatus)

	mux.HandleFunc("POST /project", s.handleProject)
}

// ── Page scaffolding ──────────────────────────────────────────────────────

// Page is embedded in every template's data so layout.html has a stable set
// of fields regardless of which page is rendering.
type Page struct {
	Title string
	Nav   string
	Flash Flash
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
	"dash":            orDash,
	"gameStatuses":    func() []string { return gameStatuses },
	"pthStatuses":     func() []string { return playthroughStatuses },
	"staleQuick":      func() []string { return staleQuickStatuses },
	"add1":            func(i int) int { return i + 1 },
	"itoa":            strconv.Itoa,
	"formatHoursFunc": formatHours,
}

// loadPage parses layout.html together with exactly one page-specific
// template file, in an isolated *template.Template each call — every page
// file defines a template named "content", and parsing them one at a time
// like this is what lets each page reuse that same name without colliding
// with any other page's.
func loadPage(name string) *template.Template {
	return template.Must(template.New("layout.html").Funcs(funcMap).
		ParseFS(templateFiles, "web/templates/layout.html", "web/templates/"+name))
}

var pageTemplates = map[string]*template.Template{
	"index":          loadPage("index.html"),
	"game_new":       loadPage("game_new.html"),
	"game_detail":    loadPage("game_detail.html"),
	"scan":           loadPage("scan.html"),
	"review":         loadPage("review.html"),
	"stale":          loadPage("stale.html"),
	"close":          loadPage("close.html"),
	"suggest_picker": loadPage("suggest_picker.html"),
	"suggest_report": loadPage("suggest_report.html"),
	"achievements":   loadPage("achievements.html"),
	"job":            loadPage("job.html"),
}

func (s *server) render(w http.ResponseWriter, name string, data any) {
	tmpl, ok := pageTemplates[name]
	if !ok {
		http.Error(w, "unknown template "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// redirectOK/redirectErr are this UI's stand-in for the TUI's ConfirmWrite
// prompt and printed result: the submitted form is already the user's
// confirmation (see writeEntry/writeSplit/writeFrontMatter in main.go), and
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
