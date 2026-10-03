package commands

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
	"go.dalton.dog/gamelog/internal/providers/exophase"
	"go.dalton.dog/gamelog/internal/providers/psn"
	"go.dalton.dog/gamelog/internal/providers/xbox"
)

// Scan covers every provider with a bulk "what has this account played" list
// — everything in model.ProviderOrder. Trackmania (Nadeo) and BTD6 (Ninja Kiwi)
// aren't achievement providers and each covers exactly one game, so there's
// nothing for a scan to discover on either.

// ScanNotice is something about one provider's part of a scan the page should
// say: that it failed (an expired PSN npsso, a private Exophase profile), or
// that it left titles out on purpose.
type ScanNotice struct {
	Provider string
	Message  string
}

func (n ScanNotice) String() string { return model.ProviderLabel(n.Provider) + ": " + n.Message }

// ScanResult is one whole-library scan.
type ScanResult struct {
	Candidates []Candidate
	Notices    []ScanNotice
	// Scanned names the providers that answered, for the page's summary line.
	Scanned []string
}

// errNoScanCredentials is returned when no provider the mode can use is
// configured at all.
var errNoScanCredentials = errors.New("no provider credentials configured; see tools/gamelog/README.md")

// Scan runs every configured provider's bulk list and turns what isn't covered
// yet into candidates. Providers are fetched concurrently — each is one to a
// few requests, and they don't share a rate limit — and a provider that fails
// becomes a notice rather than failing the scan: one expired token shouldn't
// hide every other service's games.
//
// Backlog mode is Steam alone: it's the only service with a notion of owning
// a game you haven't played. Every other list here is of games with play
// history, which is the played scan's job.
func Scan(ctx context.Context, creds Credentials, index loggedIndex, opts ScanOptions) (ScanResult, error) {
	type job struct {
		provider string
		run      func() ([]Candidate, []ScanNotice, error)
	}
	plain := func(f func() ([]Candidate, error)) func() ([]Candidate, []ScanNotice, error) {
		return func() ([]Candidate, []ScanNotice, error) {
			c, err := f()
			return c, nil, err
		}
	}
	var jobs []job
	if creds.SteamConfigured() {
		jobs = append(jobs, job{model.ProviderSteam, plain(func() ([]Candidate, error) { return ScanSteam(ctx, creds, index, opts) })})
	}
	if opts.Mode != ScanModeBacklog {
		if creds.RAConfigured() {
			jobs = append(jobs, job{model.ProviderRA, plain(func() ([]Candidate, error) {
				found, err := ScanRA(ctx, creds, index)
				if err != nil {
					return nil, err
				}
				// Resolves which base game each subset row belongs to, at one
				// per-game request per subset never seen this process. The
				// bulk endpoint carries no parent link, so this is the only
				// way a subset row can offer to attach rather than duplicate.
				return ResolveSubsets(ctx, RAClientFor(creds), found, index), nil
			})})
		}
		if creds.PSNConfigured() {
			jobs = append(jobs, job{model.ProviderPSN, plain(func() ([]Candidate, error) { return ScanPSN(ctx, creds, index) })})
		}
		if creds.XBLConfigured() {
			jobs = append(jobs, job{model.ProviderXbox, func() ([]Candidate, []ScanNotice, error) { return ScanXbox(ctx, creds, index) }})
		}
		if creds.ExophaseConfigured() {
			jobs = append(jobs,
				job{model.ProviderUbisoft, plain(func() ([]Candidate, error) {
					return ScanExophase(ctx, creds, index, model.ProviderUbisoft)
				})},
				job{model.ProviderGooglePlay, plain(func() ([]Candidate, error) {
					return ScanExophase(ctx, creds, index, model.ProviderGooglePlay)
				})},
			)
		}
	}
	if len(jobs) == 0 {
		return ScanResult{}, errNoScanCredentials
	}

	type outcome struct {
		found   []Candidate
		notices []ScanNotice
		err     error
	}
	results := make([]outcome, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, notices, err := j.run()
			results[i] = outcome{found, notices, err}
		}()
	}
	wg.Wait()

	var res ScanResult
	for _, p := range model.ProviderOrder {
		for i, j := range jobs {
			if j.provider != p {
				continue
			}
			r := results[i]
			res.Notices = append(res.Notices, r.notices...)
			if r.err != nil {
				res.Notices = append(res.Notices, ScanNotice{Provider: p, Message: r.err.Error()})
				continue
			}
			res.Scanned = append(res.Scanned, p)
			res.Candidates = append(res.Candidates, r.found...)
		}
	}
	SortCandidates(res.Candidates)
	return res, nil
}

// ScanPSN lists every trophy title on the account that isn't linked yet.
func ScanPSN(ctx context.Context, creds Credentials, index loggedIndex) ([]Candidate, error) {
	titles, err := cachedList("psn:"+creds.PSNNpsso, func() (map[string]psn.TrophyTitle, error) {
		return (&psn.PSNClient{Npsso: creds.PSNNpsso}).TrophyTitles(ctx)
	})
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for id, t := range titles {
		if index.covered(model.ProviderPSN, id) {
			continue
		}
		earned, total := t.EarnedTrophies.Sum(), t.DefinedTrophies.Sum()
		c := Candidate{
			Provider: model.ProviderPSN,
			Title:    t.TrophyTitleName,
			// "PS5,PS4" for a cross-gen set — spaced to match how front
			// matter already writes a multi-platform game.
			Platform:      strings.ReplaceAll(t.TrophyTitlePlatform, ",", ", "),
			ID:            id,
			AchievementsA: earned,
			AchievementsB: total,
			Status:        "playing",
		}
		if ts, err := time.Parse(time.RFC3339, t.LastUpdatedDateTime); err == nil {
			c.LastActivity = day(ts)
		}
		// A platinum is every base-game trophy, which is what mastered means.
		// There's no date for it in the bulk list — the archive gets the real
		// one when the game is created — so none is guessed here.
		switch {
		case t.EarnedTrophies.Platinum > 0:
			c.Finished, c.AwardKind, c.Status = true, "platinum", "mastered"
		case total > 0 && earned == total:
			c.Finished, c.AwardKind, c.Status = true, "every trophy", "mastered"
		}
		index.place(&c)
		out = append(out, c)
	}
	return out, nil
}

// ScanXbox lists every Xbox 360 title in the account's history that isn't
// linked yet. A non-360 title is left out with a notice rather than offered:
// capture reads the x360-only endpoint (see xbox.FetchRecord), so creating one
// would archive an empty record that looks like a real zero.
func ScanXbox(ctx context.Context, creds Credentials, index loggedIndex) ([]Candidate, []ScanNotice, error) {
	titles, err := cachedList("xbox:"+creds.XBLAPIKey, func() (map[string]xbox.Title, error) {
		return (&xbox.XBLClient{APIKey: creds.XBLAPIKey}).TitleHistory(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	var out []Candidate
	var modern []string
	for id, t := range titles {
		if index.covered(model.ProviderXbox, id) {
			continue
		}
		if t.MediaItemType != "Xbox360Game" {
			modern = append(modern, t.Name)
			continue
		}
		c := Candidate{
			Provider:      model.ProviderXbox,
			Title:         strings.TrimSpace(t.Name),
			Platform:      "Xbox 360",
			ID:            id,
			AchievementsA: t.Achievement.Current,
			AchievementsB: t.Achievement.Total,
			Status:        "playing",
		}
		if ts, err := time.Parse(time.RFC3339, t.TitleHistory.LastTimePlayed); err == nil {
			c.LastActivity = day(ts)
		}
		if t.Achievement.Total > 0 && t.Achievement.Current == t.Achievement.Total {
			c.Finished, c.AwardKind, c.Status = true, "every achievement", "mastered"
		}
		index.place(&c)
		out = append(out, c)
	}
	var notices []ScanNotice
	if len(modern) > 0 {
		slices.Sort(modern)
		notices = append(notices, ScanNotice{Provider: model.ProviderXbox, Message: fmt.Sprintf(
			"%d non-360 title(s) left out — capture only reads the Xbox 360 endpoint so far: %s",
			len(modern), strings.Join(modern, ", "))})
	}
	return out, notices, nil
}

// exophaseScans maps the Exophase-mirrored providers onto Exophase's
// environment slug and the platform a created game is given. Exophase calls
// Ubisoft's platform "Ubisoft" and Google's "Google Play" — both storefronts,
// not what front matter means by a platform.
var exophaseScans = map[string]struct{ env, platform string }{
	model.ProviderUbisoft:    {exophase.EnvUbisoft, "PC"},
	model.ProviderGooglePlay: {exophase.EnvGooglePlay, "Android"},
}

// ScanExophase lists every game on one Exophase-mirrored service that isn't
// linked yet. The profile reads are memoized on the shared client, so this
// and the capture that follows a create share them.
func ScanExophase(ctx context.Context, creds Credentials, index loggedIndex, provider string) ([]Candidate, error) {
	cfg, ok := exophaseScans[provider]
	if !ok {
		return nil, fmt.Errorf("%s is not mirrored through Exophase", provider)
	}
	games, err := ExophaseClientFor(creds).Games(ctx, cfg.env)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for id, g := range games {
		if index.covered(provider, id) {
			continue
		}
		c := Candidate{
			Provider:      provider,
			Title:         g.Meta.Title,
			Platform:      cfg.platform,
			ID:            id,
			AchievementsA: g.EarnedAwards,
			AchievementsB: g.TotalAwards,
			PlaytimeMins:  exophase.ParsePlaytime(g.Playtime),
			Status:        "playing",
		}
		if g.LastPlayed > 0 {
			c.LastActivity = day(time.Unix(g.LastPlayed, 0))
		}
		if g.TotalAwards > 0 && g.EarnedAwards == g.TotalAwards {
			c.Finished, c.AwardKind, c.Status = true, "every achievement", "mastered"
			if g.CompletionDate > 0 {
				c.FinishedOn = day(time.Unix(g.CompletionDate, 0))
			}
		}
		index.place(&c)
		out = append(out, c)
	}
	return out, nil
}

// ── list cache ────────────────────────────────────────────────────────────

// Housekeeping re-derives the scan on every render and every row's POST, and
// with six providers that's no longer two cheap requests: PSN alone is an
// OAuth exchange plus the list. So each provider's bulk list is memoized for
// scanCacheTTL, keyed by the credential it was read with. Only the raw lists
// are cached — which games are covered is re-derived from content/games every
// time, so a create or link drops its row immediately. The page's Re-scan
// button calls ForgetScanCache for anything played since.
const scanCacheTTL = 10 * time.Minute

var scanCache = struct {
	sync.Mutex
	entries map[string]scanCacheEntry
}{entries: map[string]scanCacheEntry{}}

type scanCacheEntry struct {
	at  time.Time
	val any
}

// cachedList returns key's memoized list, or fetches and memoizes it. A failed
// fetch isn't memoized: the next render should try again.
func cachedList[T any](key string, fetch func() (T, error)) (T, error) {
	scanCache.Lock()
	e, ok := scanCache.entries[key]
	scanCache.Unlock()
	if ok && time.Since(e.at) < scanCacheTTL {
		return e.val.(T), nil
	}
	v, err := fetch()
	if err != nil {
		return v, err
	}
	scanCache.Lock()
	scanCache.entries[key] = scanCacheEntry{at: time.Now(), val: v}
	scanCache.Unlock()
	return v, nil
}

// ForgetScanCache drops every memoized provider list, including the Exophase
// client's, so the next scan reads every provider fresh.
func ForgetScanCache(creds Credentials) {
	scanCache.Lock()
	scanCache.entries = map[string]scanCacheEntry{}
	scanCache.Unlock()
	if creds.ExophaseConfigured() {
		ExophaseClientFor(creds).Forget()
	}
}

// ── linking ───────────────────────────────────────────────────────────────

var errAlreadyLinked = errors.New("already linked")

// IsAlreadyLinked reports whether a LinkToGame error was the harmless "this
// was already done" one — the same button is easy to click twice on a page
// whose rows outlive the scan they came from.
func IsAlreadyLinked(err error) bool { return errors.Is(err, errAlreadyLinked) }

// LinkToGame writes a provider id onto an already-logged game, captures that
// provider's history into the archive, and rebuilds the game's summary — the
// scan's action for a game it found again on a second service.
//
// It never overwrites: a game already linked to a different id on that
// provider is refused, and so is an id some other game already holds. Both
// arrive from a submitted form, and either mistake would put one game's
// history on another's page.
func LinkToGame(ctx context.Context, gamesDir, slug, provider, id string, creds Credentials) (title string, err error) {
	if !slices.Contains(model.ProviderOrder, provider) {
		return "", fmt.Errorf("unknown provider %q", provider)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("linking needs an id")
	}
	games, err := model.ListGames(gamesDir)
	if err != nil {
		return "", err
	}
	for _, g := range games {
		if g.Slug != slug && g.ProviderIDs()[provider] == id {
			return "", fmt.Errorf("%s %s is already linked to %s", model.ProviderLabel(provider), id, g.Slug)
		}
	}

	path := filepath.Join(gamesDir, slug, "_index.md")
	doc, err := model.LoadDoc(path)
	if err != nil {
		return "", err
	}
	switch existing := doc.ProviderID(provider); existing {
	case id:
		return doc.FM.Title, errAlreadyLinked
	case "":
	default:
		return "", fmt.Errorf("refusing to link: %s is already linked to %s %s", slug, model.ProviderLabel(provider), existing)
	}
	if err := doc.SetProviderID(provider, id); err != nil {
		return "", err
	}
	if err := mutate.WriteFrontMatter(doc, nil, false); err != nil {
		return "", err
	}

	// A failure from here on costs the history, not the link — the link is
	// what puts this provider in the game's ProviderLinks, so the next
	// refresh picks it up.
	archiveDir := model.FindArchiveDir(gamesDir)
	links := []model.ProviderLink{{Provider: provider, ID: id}}
	if _, err := SaveAchievements(ctx, archiveDir, doc.FM.Title, links, creds); err != nil {
		return doc.FM.Title, fmt.Errorf("linked, but no achievements saved: %w", err)
	}
	if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(path), doc.ProviderLinks()); err != nil {
		return doc.FM.Title, fmt.Errorf("linked, but achievement summary not written: %w", err)
	}
	return doc.FM.Title, nil
}
