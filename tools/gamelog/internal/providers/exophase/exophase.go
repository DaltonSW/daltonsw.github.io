package exophase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// Exophase is a public aggregator that mirrors PlayStation trophy history —
// including per-trophy unlock timestamps, which is the part worth archiving.
// Sony has no public API at all, so a mirror is the only way to reach this
// data without reverse-engineering the PSN endpoints and holding an NPSSO
// cookie that expires every couple of months.
//
// Everything here reads public profile pages. There are no credentials; a
// profile just has to be public.
//
// Two sharp edges, both found against live responses rather than docs:
//
//   - Cloudflare serves an interstitial to Go's default
//     `Go-http-client/1.1` User-Agent, so requests must send a browser one.
//     Same class of trap as RetroAchievements' 403 (see raUserAgent).
//   - The JSON API's per-game `meta` is a slim subset that does NOT include
//     `canonical_id` — the real `NPWR…` PlayStation ID. That only appears in
//     the `window.playerGames` payload embedded in the profile HTML, which is
//     why this client reads both and joins them on Exophase's `master_id`.
//   - Only the *account* profile (/user/<name>/) carries the service widgets
//     that name each linked service's player id. The per-service pages
//     (/psn/user/<name>/) do not, and their per-service username differs from
//     the account name anyway — Nintendo's is an opaque hash. So the account
//     page is read first and its own `data-endpoint` links are followed.
var (
	exophaseBaseURL   = "https://www.exophase.com"
	exophaseGamesURL  = "https://api.exophase.com/public/player/%s/games"
	exophaseEarnedURL = "https://api.exophase.com/public/player/%s/game/%s/earned"
	// exophaseMediaBaseURL serves award/trophy icons — a different host than
	// the site itself (exophaseBaseURL 404s on these paths). Confirmed
	// against the profile page's own embedded config, which names it
	// `baseMediaUrl` alongside `baseUrl`/`baseApiUrl`/`baseImagesUrl` (the
	// last of which, despite the name, is not it either).
	exophaseMediaBaseURL = "https://m.exophase.com"
)

// exophaseUserAgent must look like a browser; see the Cloudflare note above.
const exophaseUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// exophasePageSize is what the games API returns per page. A short page means
// the last one.
const exophasePageSize = 50

// ExophaseClient reads one Exophase profile. Environments are Exophase's own
// service slugs — "psn", "nintendo", "uplay".
type ExophaseClient struct {
	User string // the per-environment username, e.g. "DaltonSW"
	HTTP *http.Client
}

func (c *ExophaseClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *ExophaseClient) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", exophaseUserAgent)
	req.Header.Set("Referer", "https://www.exophase.com/")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("exophase: %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("exophase: reading %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exophase: %s: unexpected status %s", url, resp.Status)
	}
	return body, nil
}

// Game is one game on a profile, as the JSON API reports it.
type Game struct {
	MasterID     int     `json:"master_id"`
	EarnedAwards int     `json:"earned_awards"`
	TotalAwards  int     `json:"total_awards"`
	Playtime     string  `json:"playtime"`
	LastPlayed   int64   `json:"lastplayed"`
	Percent      float64 `json:"percent"`
	Meta         struct {
		Title       string `json:"title"`
		CanonicalID string `json:"canonical_id"`
		Platforms   []struct {
			Name string `json:"name"`
		} `json:"platforms"`
	} `json:"meta"`

	raw json.RawMessage
}

func (g Game) Platform() string {
	if len(g.Meta.Platforms) > 0 {
		return g.Meta.Platforms[0].Name
	}
	return ""
}

var (
	// The account page lists each linked service as a widget carrying both its
	// numeric player id and the path to that service's own page.
	exophaseWidgetRe = regexp.MustCompile(`data-endpoint="([^"]+)" data-environment="([a-z]+)" data-playerid="(\d+)"`)
	// The embedded game payload is a JS single-quoted string of \uXXXX escapes.
	exophasePayloadRe = regexp.MustCompile(`(?s)window\.playerGames = '(.*?)';`)
)

// exophaseService is one linked account on a profile.
type exophaseService struct {
	PlayerID string
	Endpoint string // site-relative, e.g. "/psn/user/DaltonSW/"
}

// service resolves one environment's player id and page from the account
// profile. Both come from the same widget, so they can't disagree.
func (c *ExophaseClient) service(ctx context.Context, env string) (exophaseService, error) {
	body, err := c.get(ctx, exophaseBaseURL+"/user/"+c.User+"/")
	if err != nil {
		return exophaseService{}, err
	}
	for _, m := range exophaseWidgetRe.FindAllStringSubmatch(string(body), -1) {
		if m[2] == env {
			return exophaseService{PlayerID: m[3], Endpoint: m[1]}, nil
		}
	}
	return exophaseService{}, fmt.Errorf("exophase: profile %q has no linked %s account", c.User, env)
}

// PlayerID resolves the numeric Exophase player id for one environment.
func (c *ExophaseClient) PlayerID(ctx context.Context, env string) (string, error) {
	s, err := c.service(ctx, env)
	return s.PlayerID, err
}

// canonicalIDs maps Exophase's master_id to the provider's own game ID, read
// from the profile HTML because the JSON API omits it (see the type comment).
//
// The embedded payload holds only the first page of games. Callers must treat
// a missing entry as "cannot key this record safely" rather than inventing an
// ID — mis-keying the archive is exactly the failure the ID scheme exists to
// prevent.
func (c *ExophaseClient) canonicalIDs(ctx context.Context, svc exophaseService, env string) (map[int]string, error) {
	body, err := c.get(ctx, exophaseBaseURL+svc.Endpoint)
	if err != nil {
		return nil, err
	}
	m := exophasePayloadRe.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("exophase: no game payload on the %s profile for %q", env, c.User)
	}
	var payload struct {
		Games []Game `json:"games"`
	}
	if err := json.Unmarshal([]byte(decodeJSString(string(m[1]))), &payload); err != nil {
		return nil, fmt.Errorf("exophase: decoding %s game payload: %w", env, err)
	}
	out := map[int]string{}
	for _, g := range payload.Games {
		if g.Meta.CanonicalID != "" {
			out[g.MasterID] = g.Meta.CanonicalID
		}
	}
	return out, nil
}

// decodeJSString unescapes a JavaScript single-quoted string literal. The
// payload escapes every structural character as \uXXXX, so a naive
// "replace \uXXXX then unescape slashes" pass corrupts nested JSON strings —
// this walks left to right instead, which is what the format actually is.
func decodeJSString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		switch s[i+1] {
		case 'u':
			if i+6 <= len(s) {
				var r rune
				if _, err := fmt.Sscanf(s[i+2:i+6], "%04x", &r); err == nil {
					b.WriteRune(r)
					i += 6
					continue
				}
			}
			b.WriteByte(s[i+1])
			i += 2
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		default:
			b.WriteByte(s[i+1])
			i += 2
		}
	}
	return b.String()
}

// Games lists every game on one environment of the profile, keyed by the
// provider's own ID (an NPWR… for PSN, a title id for Nintendo). Games whose
// canonical ID can't be resolved are omitted rather than guessed at.
func (c *ExophaseClient) Games(ctx context.Context, env string) (map[string]Game, error) {
	svc, err := c.service(ctx, env)
	if err != nil {
		return nil, err
	}
	playerID := svc.PlayerID
	canon, err := c.canonicalIDs(ctx, svc, env)
	if err != nil {
		return nil, err
	}

	out := map[string]Game{}
	for page := 1; ; page++ {
		url := fmt.Sprintf(exophaseGamesURL, playerID)
		if page > 1 {
			url += fmt.Sprintf("?page=%d", page)
		}
		body, err := c.get(ctx, url)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Success bool              `json:"success"`
			Games   []json.RawMessage `json:"games"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("exophase: decoding %s games: %w", env, err)
		}
		if !payload.Success && len(payload.Games) == 0 {
			return nil, fmt.Errorf("exophase: %s games list for player %s returned no data", env, playerID)
		}

		before := len(out)
		for _, rawGame := range payload.Games {
			var g Game
			if err := json.Unmarshal(rawGame, &g); err != nil {
				return nil, fmt.Errorf("exophase: decoding %s game: %w", env, err)
			}
			g.raw = rawGame
			id := canon[g.MasterID]
			if id == "" {
				continue
			}
			out[id] = g
		}
		// A short page ends the walk; so does a page that added nothing new,
		// which is what a server ignoring ?page= looks like.
		if len(payload.Games) < exophasePageSize || len(out) == before {
			break
		}
	}
	return out, nil
}

// Award is one earned trophy. Only earned ones are listed — the
// locked denominator comes from the game's TotalAwards. The feed carries no
// display name or description (see fetchRecord), but it does carry icons —
// site-relative paths in five sizes; "m" is used as a reasonable match for
// the badge-sized rows RA/Steam icons already render at.
type Award struct {
	Slug      string `json:"slug"`
	Earned    string `json:"earned"`
	Timestamp int64  `json:"timestamp"`
	Icons     struct {
		Medium string `json:"m"`
	} `json:"icons"`
}

// IconURL resolves this award's medium icon to an absolute URL, or "" if the
// feed didn't include one.
func (a Award) IconURL() string {
	if a.Icons.Medium == "" {
		return ""
	}
	return exophaseMediaBaseURL + a.Icons.Medium
}

// Earned returns the unlock list for one game on the profile.
func (c *ExophaseClient) Earned(ctx context.Context, playerID string, masterID int) ([]Award, json.RawMessage, error) {
	body, err := c.get(ctx, fmt.Sprintf(exophaseEarnedURL, playerID, fmt.Sprint(masterID)))
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		List []Award `json:"list"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, fmt.Errorf("exophase: decoding awards for game %d: %w", masterID, err)
	}
	return payload.List, body, nil
}

// EnvUbisoft is Exophase's slug for Ubisoft Connect (formerly Uplay).
const EnvUbisoft = "uplay"

var exophasePlaytimeRe = regexp.MustCompile(`(?:(\d+)h)?\s*(?:(\d+)m)?`)

// ParsePlaytime turns "15h 6m" into minutes. Exophase reports
// playtime as prose, not a number.
func ParsePlaytime(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	m := exophasePlaytimeRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	var mins int
	if m[1] != "" {
		var h int
		fmt.Sscanf(m[1], "%d", &h)
		mins += h * 60
	}
	if m[2] != "" {
		var x int
		fmt.Sscanf(m[2], "%d", &x)
		mins += x
	}
	return mins
}

// FetchUbisoftRecord builds an archive record for one Ubisoft Connect game.
// Ubisoft has no first-party achievements API at all — unlike PlayStation,
// which moved to Sony's own trophy API (see internal/providers/psn) — so this
// mirror remains the only source. Source is set to "exophase" on the
// resulting record, same as PSN's records were while it was mirrored here.
func FetchUbisoftRecord(ctx context.Context, client *ExophaseClient, gameID string) (*model.ProviderRecord, error) {
	return fetchRecord(ctx, client, EnvUbisoft, "Ubisoft", gameID)
}

// fetchRecord is FetchUbisoftRecord's body, kept separate from it only
// because it used to be shared with FetchPSNRecord too — env/label are still
// parameters rather than hardcoded so that shape doesn't need to change if
// another Exophase-mirrored provider shows up.
func fetchRecord(ctx context.Context, client *ExophaseClient, env, label, id string) (*model.ProviderRecord, error) {
	rec := &model.ProviderRecord{ID: id, LastAttempt: today(), Source: "exophase"}

	playerID, err := client.PlayerID(ctx, env)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	games, err := client.Games(ctx, env)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	g, ok := games[id]
	if !ok {
		rec.LastError = fmt.Sprintf("no %s game %s on this profile", label, id)
		return rec, nil
	}

	rec.Total = g.TotalAwards
	rec.Platform = g.Platform()
	rec.Completion = fmt.Sprintf("%.0f%%", g.Percent)
	if g.LastPlayed > 0 {
		rec.LastPlayed = stamp(time.Unix(g.LastPlayed, 0))[:10]
	}
	rec.PlaytimeMins = ParsePlaytime(g.Playtime)

	awards, raw, err := client.Earned(ctx, playerID, g.MasterID)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	for _, a := range awards {
		if a.Timestamp <= 0 {
			continue
		}
		rec.Achievements = append(rec.Achievements, model.ArchivedAchievement{
			// The awards feed carries no display name, only a slug. Using it
			// for both keeps Key provider-stable and avoids inventing a title
			// the provider never gave us; `raw` holds everything for later.
			Key:      a.Slug,
			Name:     a.Slug,
			Unlocked: true,
			Date:     stamp(time.Unix(a.Timestamp, 0)),
			Icon:     a.IconURL(),
		})
	}
	rec.Raw = raw
	rec.Fetched = today()
	rec.Summarize()
	return rec, nil
}

// today formats the current instant as a calendar date in the site's
// timezone, matching forms.Today's convention.
func today() string {
	return time.Now().In(model.SiteLocation).Format("2006-01-02")
}

// stamp renders an instant the way the archive stores dates.
func stamp(t time.Time) string {
	return t.In(model.SiteLocation).Format(time.RFC3339)
}
