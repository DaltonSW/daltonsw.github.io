package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dalton.dog/gamelog/internal/model"
)

func postProviders(t *testing.T, s *server, slug string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/games/"+slug+"/providers", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", slug)
	rec := httptest.NewRecorder()
	s.handleUpdateProviders(rec, req)
	return rec
}

// flashError is the error the handler redirected with, "" if it saved.
func flashError(rec *httptest.ResponseRecorder) string {
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		return err.Error()
	}
	return u.Query().Get("error")
}

// The provider-IDs form is built from model.Providers: every provider's input
// is named by its key, RA and Steam take a pasted URL, the rest are stored as
// typed, and clearing a set id is a declared removal rather than a refused write.
func TestUpdateProviders_SetsAndClearsEveryProvider(t *testing.T) {
	gamesDir := filepath.Join(t.TempDir(), "content", "games")
	dir := filepath.Join(gamesDir, "hades")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "_index.md"), []byte("---\ntitle: Hades\nstatus: playing\n---\n"), 0o644)
	s := &server{gamesDir: gamesDir}

	form := url.Values{
		model.ProviderRA:         {"https://retroachievements.org/game/4650"},
		model.ProviderSteam:      {"https://store.steampowered.com/app/1145360/Hades/"},
		model.ProviderPSN:        {" NPWR16532_00 "},
		model.ProviderUbisoft:    {"12345"},
		model.ProviderXbox:       {"1480657033"},
		model.ProviderGooglePlay: {"com.example.game"},
	}
	if msg := flashError(postProviders(t, s, "hades", form)); msg != "" {
		t.Fatalf("saving failed: %s", msg)
	}
	doc, err := model.LoadDoc(filepath.Join(dir, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		model.ProviderRA: "4650", model.ProviderSteam: "1145360", model.ProviderPSN: "NPWR16532_00",
		model.ProviderUbisoft: "12345", model.ProviderXbox: "1480657033", model.ProviderGooglePlay: "com.example.game",
	}
	for p, id := range want {
		if got := doc.ProviderID(p); got != id {
			t.Errorf("%s id = %q, want %q", p, got, id)
		}
	}

	// Clearing every field removes the ids, including the omitempty keys.
	if msg := flashError(postProviders(t, s, "hades", url.Values{})); msg != "" {
		t.Fatalf("clearing was refused: %s", msg)
	}
	doc, err = model.LoadDoc(filepath.Join(dir, "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if ids := doc.ProviderIDs(); len(ids) != 0 {
		t.Errorf("ids after clearing = %v, want none", ids)
	}

	// A malformed numeric id is refused, naming the field.
	msg := flashError(postProviders(t, s, "hades", url.Values{model.ProviderSteam: {"not-a-number"}}))
	if !strings.Contains(msg, "Steam appid") {
		t.Errorf("malformed Steam id: flash error = %q", msg)
	}

	// The page's form has one input per provider, filled from the saved ids.
	postProviders(t, s, "hades", form)
	tmpl, err := parsePage(pageSpecs["game_detail"].name, pageSpecs["game_detail"].partials...)
	if err != nil {
		t.Fatal(err)
	}
	doc, pf, err := s.loadGame("hades")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	req := httptest.NewRequest(http.MethodGet, "/games/hades", nil)
	if err := tmpl.ExecuteTemplate(&buf, "layout", s.buildGameDetail(req, "hades", doc, pf)); err != nil {
		t.Fatal(err)
	}
	for _, p := range model.Providers() {
		if !strings.Contains(buf.String(), `name="`+p.Key+`" value="`+want[p.Key]+`"`) {
			t.Errorf("provider form has no %s input holding %q", p.Key, want[p.Key])
		}
	}
	if !strings.Contains(buf.String(), `id="suggest"`) {
		t.Error("a game with provider ids should offer suggested dates")
	}
}
