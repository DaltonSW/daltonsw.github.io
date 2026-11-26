package commands

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/providers/exophase"
	"go.dalton.dog/gamelog/internal/providers/psn"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
	"go.dalton.dog/gamelog/internal/providers/steam"
	"go.dalton.dog/gamelog/internal/providers/xbox"
)

// raRecentCache memoizes the user-level GetUserRecentlyPlayedGames list for
// scanCacheTTL (and until ForgetScanCache): at RA's ~1.2s throttle, re-walking
// its pages per game would dominate a bulk run, but a long-running serve must
// not freeze last_played at its first fetch.
var raRecentCache = struct {
	sync.Mutex
	byUser map[string]raRecentEntry
}{byUser: map[string]raRecentEntry{}}

type raRecentEntry struct {
	at   time.Time
	byID map[string]*retroachievements.RARecentGame
}

// raRecentlyPlayed returns last-played dates keyed by RA game id. A failure
// isn't fatal: last_played just falls back to the newest unlock.
func raRecentlyPlayed(ctx context.Context, client *retroachievements.RAClient, username string) map[string]*retroachievements.RARecentGame {
	raRecentCache.Lock()
	defer raRecentCache.Unlock()
	if e, ok := raRecentCache.byUser[username]; ok && time.Since(e.at) < scanCacheTTL {
		return e.byID
	}

	games, err := client.GetRecentlyPlayed(ctx)
	if err != nil {
		// Cached anyway (for the TTL only), so this warns once per run rather than per game.
		fmt.Fprintf(os.Stderr, "  (retroachievements last-played unavailable: %v)\n", err)
	}
	byID := map[string]*retroachievements.RARecentGame{}
	for i := range games {
		g := games[i]
		byID[g.ID()] = &g
	}
	raRecentCache.byUser[username] = raRecentEntry{at: time.Now(), byID: byID}
	return byID
}

// forgetRARecent drops every memoized recently-played list.
func forgetRARecent() {
	raRecentCache.Lock()
	clear(raRecentCache.byUser)
	raRecentCache.Unlock()
}

// savedRecord is one provider's outcome from a fetch-and-archive run.
type savedRecord struct {
	Provider string
	ID       string
	Subset   bool
	Path     string
	Record   *model.ArchiveRecord
	// Skipped means the provider reported nothing new since the last fetch, so
	// no per-game request was made and Record is simply what was already
	// archived.
	Skipped bool
}

// Label names the record in a progress line. Every link on a game reports the
// same provider name until subsets exist, at which point "retroachievements"
// three times over says nothing about which set moved.
func (s savedRecord) Label() string {
	if !s.Subset {
		return s.Provider
	}
	name := s.ID
	if s.Record != nil {
		if _, sub, ok := model.SplitRASubsetTitle(s.Record.Title); ok {
			name = sub
		}
	}
	return s.Provider + " subset " + name
}

// saveAchievements fetches from every configured provider the game is linked
// to — not just the first — and merges each result into its own archive file.
//
// It walks links in order rather than folding them into one id per provider,
// because a RetroAchievements subset is a second RA id on the same game (see
// model.FrontMatter.RASubsets). Order is BuildProviderLinks' order, which is
// ProviderOrder with each game's subsets behind its base RA set.
//
// An error aborts the rest of the walk with whatever was already written left
// in place. That's safe, and better than discarding it: every write is a merge
// that can only add (see model.mergeProvider), so a half-finished refresh is
// simply a refresh of fewer sets.
//
// SkipUnchanged is for bulk refreshes: a Steam game whose playtime and
// last-played date match the archive is not re-fetched (see steamUnchanged).
func SaveAchievements(ctx context.Context, archiveDir, title string, links []model.ProviderLink, creds Credentials, opts ...SaveOption) ([]savedRecord, error) {
	var o saveOptions
	for _, opt := range opts {
		opt(&o)
	}
	var saved []savedRecord
	var unconfigured []string // providers skipped for missing credentials

	for _, link := range links {
		if link.ID == "" {
			continue
		}
		var (
			rec *model.ProviderRecord
			err error
		)
		switch link.Provider {
		case model.ProviderRA:
			if !creds.RAConfigured() {
				unconfigured = append(unconfigured, link.Provider)
				continue
			}
			client := raClient(creds)
			rec, err = retroachievements.FetchRecord(ctx, client, link.ID,
				raRecentlyPlayed(ctx, client, creds.RAUsername)[link.ID])
		case model.ProviderSteam:
			if !creds.SteamConfigured() {
				unconfigured = append(unconfigured, link.Provider)
				continue
			}
			steamOwned, lerr := steamOwnedGames(ctx, creds)
			if lerr != nil {
				return nil, fmt.Errorf("steam library listing: %w", lerr)
			}
			var owned *steam.SteamOwnedGame
			for i := range steamOwned {
				if strconv.Itoa(steamOwned[i].AppID) == link.ID {
					owned = &steamOwned[i]
					break
				}
			}
			if o.skipUnchanged && owned != nil {
				if prev, lerr := model.LoadRecord(archiveDir, link.Provider, link.ID); lerr == nil && steamUnchanged(prev, owned) {
					saved = append(saved, savedRecord{Provider: link.Provider, ID: link.ID, Subset: link.Subset,
						Path: model.RecordPath(archiveDir, link.Provider, link.ID), Record: prev, Skipped: true})
					continue
				}
			}
			rec, err = steam.FetchRecord(ctx, steamClient(creds), link.ID, owned)
		case model.ProviderPSN:
			if !creds.PSNConfigured() {
				unconfigured = append(unconfigured, link.Provider)
				continue
			}
			titles, lerr := psnTrophyTitles(ctx, creds)
			if lerr != nil {
				rec = listingFailed(link.ID, lerr)
			} else {
				rec, err = psn.FetchRecord(ctx, psnClient(creds), link.ID, titles)
			}
		case model.ProviderUbisoft:
			if !creds.ExophaseConfigured() {
				unconfigured = append(unconfigured, link.Provider)
				continue
			}
			rec, err = exophase.FetchUbisoftRecord(ctx, ExophaseClientFor(creds), link.ID)
		case model.ProviderXbox:
			if !creds.XBLConfigured() {
				unconfigured = append(unconfigured, link.Provider)
				continue
			}
			titles, lerr := xboxTitleHistory(ctx, creds)
			if lerr != nil {
				rec = listingFailed(link.ID, lerr)
			} else {
				rec, err = xbox.FetchRecord(ctx, xblClient(creds), link.ID, titles)
			}
		case model.ProviderGooglePlay:
			if !creds.ExophaseConfigured() {
				unconfigured = append(unconfigured, link.Provider)
				continue
			}
			rec, err = exophase.FetchGooglePlayRecord(ctx, ExophaseClientFor(creds), link.ID)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}

		// A subset's record is titled by RetroAchievements, not by the site.
		// The game's own title names the *base* set, so writing it here would
		// erase the only place "Mouse Alley" is recorded — and a refresh of
		// the game runs through this same loop for every set it has. Blank
		// (a failed fetch) leaves whatever SaveRecord already had.
		recordTitle := title
		if link.Subset {
			recordTitle = rec.ProviderTitle
		}
		path, err := model.SaveRecord(archiveDir, link.Provider, link.ID, recordTitle, rec)
		if err != nil {
			return nil, err
		}
		stored, err := model.LoadRecord(archiveDir, link.Provider, link.ID)
		if err != nil {
			return nil, err
		}
		saved = append(saved, savedRecord{Provider: link.Provider, ID: link.ID, Subset: link.Subset, Path: path, Record: stored})
	}

	if len(saved) == 0 {
		if len(unconfigured) > 0 {
			return nil, errUnconfigured(unconfigured)
		}
		return nil, fmt.Errorf("no provider link on this game")
	}
	return saved, nil
}

// listingFailed is the record for a game whose provider's title list couldn't
// be read: an attempt with an error, which the merge keeps away from the data.
func listingFailed(id string, err error) *model.ProviderRecord {
	return &model.ProviderRecord{ID: id, LastAttempt: time.Now().In(model.SiteLocation).Format("2006-01-02"), LastError: err.Error()}
}

// The account-wide title lists, shared with the scan through cachedList so a
// Catch up run reads each once.
func steamOwnedGames(ctx context.Context, creds Credentials) ([]steam.SteamOwnedGame, error) {
	return cachedList("steam:"+creds.SteamID, func() ([]steam.SteamOwnedGame, error) {
		return steamClient(creds).GetOwnedGames(ctx)
	})
}

func psnTrophyTitles(ctx context.Context, creds Credentials) (map[string]psn.TrophyTitle, error) {
	return cachedList("psn:"+creds.PSNNpsso, func() (map[string]psn.TrophyTitle, error) {
		return psnClient(creds).TrophyTitles(ctx)
	})
}

func xboxTitleHistory(ctx context.Context, creds Credentials) (map[string]xbox.Title, error) {
	return cachedList("xbox:"+creds.XBLAPIKey, func() (map[string]xbox.Title, error) {
		return xblClient(creds).TitleHistory(ctx)
	})
}

// credentialVars names the .env keys each achievement provider needs.
var credentialVars = map[string]string{
	model.ProviderRA:         "RA_USERNAME and RA_API_KEY",
	model.ProviderSteam:      "STEAM_API_KEY and STEAM_ID",
	model.ProviderPSN:        "PSN_NPSSO",
	model.ProviderXbox:       "XBLIO_API_KEY",
	model.ProviderUbisoft:    "EXOPHASE_USER",
	model.ProviderGooglePlay: "EXOPHASE_USER",
}

// errUnconfigured says which linked providers were skipped and what to set.
func errUnconfigured(providers []string) error {
	parts := make([]string, 0, len(providers))
	for _, p := range slices.Compact(providers) {
		parts = append(parts, fmt.Sprintf("%s (set %s in tools/gamelog/.env)", model.ProviderLabel(p), credentialVars[p]))
	}
	return fmt.Errorf("credentials not configured: %s", strings.Join(parts, "; "))
}

type saveOptions struct{ skipUnchanged bool }

// SaveOption tunes SaveAchievements.
type SaveOption func(*saveOptions)

// SkipUnchanged lets SaveAchievements leave a Steam game alone when the
// library listing shows nothing has happened to it since the last fetch.
var SkipUnchanged SaveOption = func(o *saveOptions) { o.skipUnchanged = true }

// steamUnchanged reports whether the already-archived Steam record is still
// current against the library listing, which is fetched once per run anyway.
// Any play moves playtime_forever, so equal playtime and last-played day mean
// no new unlocks are expected. It errs toward fetching: nothing archived yet,
// a never-successful fetch, or a failed last attempt all count as changed.
// Achievements unlocked offline can sync later without moving either value, so
// a full (non-skipping) refresh is still worth running occasionally.
func steamUnchanged(prev *model.ArchiveRecord, owned *steam.SteamOwnedGame) bool {
	if prev == nil || prev.Fetched == "" || prev.LastError != "" {
		return false
	}
	if hiddenGapsFillable(prev) {
		return false
	}
	if prev.PlaytimeMins != owned.PlaytimeMins {
		return false
	}
	lastPlayed := ""
	if owned.LastPlayed > 0 {
		lastPlayed = time.Unix(owned.LastPlayed, 0).In(model.SiteLocation).Format("2006-01-02")
	}
	return prev.LastPlayed == lastPlayed
}

// hiddenGapsFillable reports whether the record has a hidden achievement with
// no description that the Steam client's local cache can now supply — e.g. the
// game was launched, so Steam fetched its schema, after the last fetch. Without
// this a bulk refresh would skip the game forever, as playtime hasn't moved.
func hiddenGapsFillable(prev *model.ArchiveRecord) bool {
	var local map[string]steam.LocalAchievement
	for _, a := range prev.Achievements {
		if !a.Hidden || a.Description != "" {
			continue
		}
		if local == nil {
			if local = steam.LoadLocalSchema(prev.ID); local == nil {
				return false
			}
		}
		if local[a.Key].Description != "" {
			return true
		}
	}
	return false
}

// DescribeUnlocks is a record's unlock count for a refresh report. A record
// with no achievements says so instead of printing "0/0 unlocked".
func DescribeUnlocks(p *model.ArchiveRecord) string {
	var out string
	switch {
	case p.Total == 0:
		out = "no achievements"
	case p.First == "":
		out = fmt.Sprintf("%d/%d unlocked", p.Unlocked, p.Total)
	default:
		out = fmt.Sprintf("%d/%d unlocked, %s to %s", p.Unlocked, p.Total, p.First, p.Last)
	}
	if n := ReportedStats(p); n > 0 {
		out += fmt.Sprintf(", %d stat%s", n, plural(n))
	}
	return out
}

// ReportedStats counts a record's stats that carry a value. A stat the game
// defines but the provider reported nothing for isn't one a refresh captured.
func ReportedStats(p *model.ArchiveRecord) int {
	n := 0
	for _, s := range p.Stats {
		if s.Value != nil {
			n++
		}
	}
	return n
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
