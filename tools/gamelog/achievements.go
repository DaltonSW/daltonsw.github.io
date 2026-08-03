package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/charmbracelet/huh"
	"gopkg.in/yaml.v3"
)

// archiveDirName is the top-level directory holding captured provider
// history, one file per provider per game at <provider>/<id>.json.
//
// It sits outside content/ on purpose. Storing it in a game's bundle keyed it
// to the slug — a presentation decision — so renaming a game moved the
// archive and deleting a game destroyed unlock history that may be
// impossible to re-fetch. Provider IDs never change, and being outside the
// content tree also means Hugo never parses `raw` at build time.
const archiveDirName = "archive"

const (
	providerRA    = "retroachievements"
	providerSteam = "steam"
	providerPSN   = "psn"
)

// providerLink pairs a provider with this game's ID on it. Everything that
// walks a game's archive takes a slice of these rather than a positional
// (raID, steamAppID) pair, so adding a provider doesn't mean editing every
// signature that touches the archive.
type providerLink struct {
	Provider string
	ID       string
}

// providerOrder fixes the order records are written and reported in, so
// output and tests don't depend on map iteration.
var providerOrder = []string{providerRA, providerSteam, providerPSN}

// ArchivedAchievement is one achievement as recorded. Locked ones are kept
// too: they carry the denominator, and knowing an achievement exists but is
// unearned is itself information — including when a game later adds more.
type ArchivedAchievement struct {
	Key         string `json:"key"` // provider-stable id: RA numeric id, Steam apiname
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Unlocked    bool   `json:"unlocked"`
	Date        string `json:"date,omitempty"` // RFC3339 with the site's offset
	Points      int    `json:"points,omitempty"`
	Hardcore    bool   `json:"hardcore,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

// ProviderRecord is everything one service knows about one game.
type ProviderRecord struct {
	ID      string `json:"id"`
	Fetched string `json:"fetched"` // last time this record was successfully updated

	// LastAttempt/LastError record a refresh that returned nothing usable.
	// The data above is left untouched in that case — see mergeProvider.
	LastAttempt string `json:"last_attempt,omitempty"`
	LastError   string `json:"last_error,omitempty"`

	Unlocked  int    `json:"unlocked"`
	Total     int    `json:"total"`
	First     string `json:"first,omitempty"`
	Last      string `json:"last,omitempty"`
	AwardKind string `json:"award_kind,omitempty"`
	AwardDate string `json:"award_date,omitempty"`

	Platform   string `json:"platform,omitempty"`
	Icon       string `json:"icon,omitempty"`
	Completion string `json:"completion,omitempty"`

	// Source names where the data came from when that isn't the provider's
	// own API — "exophase" for the PlayStation mirror. Without it a later run
	// against Sony's endpoints couldn't tell first-hand data from relayed
	// data, and the merge rules would have no way to prefer the former.
	Source string `json:"source,omitempty"`

	PlaytimeMins int            `json:"playtime_mins,omitempty"`
	Playtime     map[string]int `json:"playtime,omitempty"` // per-device breakdown
	LastPlayed   string         `json:"last_played,omitempty"`

	Achievements []ArchivedAchievement `json:"achievements"`

	// Raw is the provider's verbatim response. Kept so a field we didn't
	// think to normalise is still recoverable without re-fetching — which
	// may be impossible if access is later lost.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// ArchiveRecord is one provider's history for one game — the whole of
// archive/<provider>/<id>.json.
//
// Title is the provider's own name for the game, not the site's. A record
// whose ID no longer matches any game in content/ is expected (the entry was
// never created, or was deleted) and must stay identifiable on its own.
//
// Nothing here links the two providers of a dual-tracked game; the
// retroachievements_id and steam_appid in front matter do. That is the right
// place for it — "these two records are the same game" is a judgement a human
// made, not something either provider reported.
type ArchiveRecord struct {
	Provider string `json:"provider"`
	Title    string `json:"title"`
	Updated  string `json:"updated"`

	// ProviderRecord supplies id, fetched, the achievement list and raw.
	ProviderRecord
}

// summarize recomputes the derived fields from the achievement list.
func (p *ProviderRecord) summarize() {
	sort.Slice(p.Achievements, func(i, j int) bool {
		a, b := p.Achievements[i], p.Achievements[j]
		if a.Unlocked != b.Unlocked {
			return a.Unlocked // unlocked first, in date order
		}
		if a.Unlocked {
			return a.Date < b.Date
		}
		return a.Key < b.Key
	})

	p.Unlocked, p.First, p.Last = 0, "", ""
	for _, a := range p.Achievements {
		if !a.Unlocked || a.Date == "" {
			continue
		}
		p.Unlocked++
		if p.First == "" || a.Date < p.First {
			p.First = a.Date
		}
		if a.Date > p.Last {
			p.Last = a.Date
		}
	}
	if len(p.First) >= 10 {
		p.First = p.First[:10]
	}
	if len(p.Last) >= 10 {
		p.Last = p.Last[:10]
	}
	if p.Total < len(p.Achievements) {
		p.Total = len(p.Achievements)
	}
}

// mergeProvider folds a fresh fetch into what's already recorded. The rule
// that matters: **a refresh must never be able to lose data.** An unlock
// recorded once stays recorded even if the provider later denies it —
// profiles go private, games get delisted, rate limits return empty bodies,
// and none of those should erase history.
func mergeProvider(old, fresh *ProviderRecord) *ProviderRecord {
	if old == nil {
		fresh.summarize()
		return fresh
	}
	if fresh == nil {
		return old
	}

	merged := *old
	merged.LastAttempt = fresh.LastAttempt
	merged.LastError = fresh.LastError

	// A fetch that produced nothing usable updates only the attempt
	// metadata; the existing record is left entirely alone.
	if fresh.LastError != "" || len(fresh.Achievements) == 0 {
		if fresh.ID != "" {
			merged.ID = fresh.ID
		}
		return &merged
	}

	merged.Fetched = fresh.Fetched
	merged.ID = firstNonEmpty(fresh.ID, old.ID)
	merged.Platform = firstNonEmpty(fresh.Platform, old.Platform)
	merged.Icon = firstNonEmpty(fresh.Icon, old.Icon)
	merged.Completion = firstNonEmpty(fresh.Completion, old.Completion)
	// Deliberately assigned, not firstNonEmpty: a first-party fetch reports
	// Source "" and must be able to clear an earlier "exophase", otherwise a
	// record would claim to be relayed forever after a first-party refresh.
	merged.Source = fresh.Source
	merged.AwardKind = firstNonEmpty(fresh.AwardKind, old.AwardKind)
	merged.AwardDate = firstNonEmpty(fresh.AwardDate, old.AwardDate)
	merged.LastPlayed = firstNonEmpty(fresh.LastPlayed, old.LastPlayed)
	if fresh.Total > merged.Total {
		merged.Total = fresh.Total
	}
	if fresh.PlaytimeMins > merged.PlaytimeMins {
		merged.PlaytimeMins = fresh.PlaytimeMins
	}
	if len(fresh.Playtime) > 0 {
		merged.Playtime = fresh.Playtime
	}
	if len(fresh.Raw) > 0 {
		merged.Raw = fresh.Raw
	}

	byKey := make(map[string]ArchivedAchievement, len(old.Achievements))
	order := make([]string, 0, len(old.Achievements))
	for _, a := range old.Achievements {
		if _, seen := byKey[a.Key]; !seen {
			order = append(order, a.Key)
		}
		byKey[a.Key] = a
	}
	for _, a := range fresh.Achievements {
		prev, seen := byKey[a.Key]
		if !seen {
			order = append(order, a.Key)
			byKey[a.Key] = a
			continue
		}
		byKey[a.Key] = mergeAchievement(prev, a)
	}

	merged.Achievements = make([]ArchivedAchievement, 0, len(order))
	for _, k := range order {
		merged.Achievements = append(merged.Achievements, byKey[k])
	}
	merged.summarize()
	return &merged
}

// mergeAchievement keeps the union of what's known. An unlock, once seen, is
// never retracted, and its original date is preferred over a later one.
func mergeAchievement(old, fresh ArchivedAchievement) ArchivedAchievement {
	out := fresh
	out.Name = firstNonEmpty(fresh.Name, old.Name)
	out.Description = firstNonEmpty(fresh.Description, old.Description)
	out.Icon = firstNonEmpty(fresh.Icon, old.Icon)
	if fresh.Points == 0 {
		out.Points = old.Points
	}

	switch {
	case old.Unlocked && !fresh.Unlocked:
		// The provider no longer reports this as earned. Trust the archive.
		out.Unlocked = true
		out.Date = old.Date
		out.Hardcore = old.Hardcore
	case old.Unlocked && fresh.Unlocked:
		out.Date = firstNonEmpty(old.Date, fresh.Date)
		out.Hardcore = old.Hardcore || fresh.Hardcore
	}
	return out
}

// RecordPath is where one provider's history for one game lives.
func RecordPath(archiveDir, provider, id string) string {
	return filepath.Join(archiveDir, provider, id+".json")
}

// findArchiveDir locates the archive next to content/, deriving the repo root
// from where content/games was found rather than searching again.
func findArchiveDir(gamesDir string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(gamesDir)), archiveDirName)
}

// LoadRecord reads one provider's archived history, returning nil (not an
// error) when nothing has been captured for it yet.
func LoadRecord(archiveDir, provider, id string) (*ArchiveRecord, error) {
	path := RecordPath(archiveDir, provider, id)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rec ArchiveRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &rec, nil
}

// SaveRecord folds a fresh fetch into whatever is already archived for this
// provider and game, and writes the result. The merge is what guarantees a
// refresh can only ever add — see mergeProvider.
func SaveRecord(archiveDir, provider, id, title string, fresh *ProviderRecord) (string, error) {
	existing, err := LoadRecord(archiveDir, provider, id)
	if err != nil {
		return "", err
	}

	rec := ArchiveRecord{Provider: provider}
	var old *ProviderRecord
	if existing != nil {
		rec = *existing
		old = &existing.ProviderRecord
	}
	if title != "" {
		rec.Title = title
	}
	rec.Provider = provider
	rec.ProviderRecord = *mergeProvider(old, fresh)
	if rec.ID == "" {
		rec.ID = id
	}
	rec.Updated = time.Now().In(siteLocation).Format("2006-01-02")

	blob, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return "", err
	}
	path := RecordPath(archiveDir, provider, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, append(blob, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// achievementSummaryFilename is a slim, raw-free projection of a game's
// achievement counts, written into its content bundle so Hugo can display
// them without reading archive/ directly. Covered by the same
// publishResources: false cascade as playthroughs.yaml.
const achievementSummaryFilename = "achievement-summary.yaml"

// AchievementSummary is the per-game stat rollup Hugo reads at build time:
// combined unlocked/total across every provider a game is linked to (e.g.
// RetroAchievements 12/40 + Steam 46/54 -> 58/94), total playtime, and the
// most recent activity date — both Steam-only stats, surfaced the same way.
type AchievementSummary struct {
	Unlocked     int    `yaml:"unlocked"`
	Total        int    `yaml:"total"`
	PlaytimeMins int    `yaml:"playtime_mins,omitempty"`
	LastPlayed   string `yaml:"last_played,omitempty"`

	// Providers is the same numbers before they were added together. A game
	// played on two platforms has two separate achievement sets, and summing
	// them reads as one impossible set — Persona 5 Royal is 53/53 on PS4 and
	// 53/53 on Steam, not 106/106 of anything.
	//
	// The totals above are kept because they are still the right answer to
	// "how many achievements in all", and because the archive is shaped for
	// querying rather than for one layout. Which of the two a given view uses
	// is a display decision.
	Providers []ProviderBreakdown `yaml:"providers,omitempty"`
}

// ProviderBreakdown is one provider's contribution to a game's summary.
type ProviderBreakdown struct {
	Provider string `yaml:"provider"`
	// Platform is the provider's own name for where it was played (PS4, PC,
	// GameCube). It's what a reader actually recognises — "psn" is plumbing.
	Platform     string `yaml:"platform,omitempty"`
	Unlocked     int    `yaml:"unlocked"`
	Total        int    `yaml:"total"`
	PlaytimeMins int    `yaml:"playtime_mins,omitempty"`
	LastPlayed   string `yaml:"last_played,omitempty"`
}

func achievementSummaryPath(gameDir string) string {
	return filepath.Join(gameDir, achievementSummaryFilename)
}

// writeAchievementSummary recomputes the projection from whatever is
// currently archived for either provider, not just a record just fetched.
// A game with nothing archived gets no file; a stale one is removed. wrote
// reports whether a file now exists, so callers can tally writes vs. no-ops.
func writeAchievementSummary(archiveDir, gameDir string, links []providerLink) (wrote bool, err error) {
	var unlocked, total, playtimeMins int
	var lastPlayed string
	var breakdown []ProviderBreakdown
	for _, link := range links {
		provider, id := link.Provider, link.ID
		if id == "" {
			continue
		}
		rec, err := LoadRecord(archiveDir, provider, id)
		if err != nil {
			return false, err
		}
		if rec == nil {
			continue
		}
		breakdown = append(breakdown, ProviderBreakdown{
			Provider:     provider,
			Platform:     rec.Platform,
			Unlocked:     rec.Unlocked,
			Total:        rec.Total,
			PlaytimeMins: rec.PlaytimeMins,
			LastPlayed:   firstNonEmpty(rec.LastPlayed, rec.Last),
		})
		unlocked += rec.Unlocked
		total += rec.Total
		playtimeMins += rec.PlaytimeMins
		// RetroAchievements has no "last played" signal — the closest proxy
		// is the most recent achievement unlock, already tracked as Last.
		// Steam's LastPlayed (rtime_last_played) and PSN's are the real
		// thing when present, so prefer them, but fall back to Last for a
		// record captured before playtime was linked in.
		for _, d := range []string{rec.LastPlayed, rec.Last} {
			if d != "" && d > lastPlayed {
				lastPlayed = d
			}
		}
	}

	path := achievementSummaryPath(gameDir)
	if total == 0 && playtimeMins == 0 && lastPlayed == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}

	out, err := yaml.Marshal(AchievementSummary{
		Unlocked: unlocked, Total: total, PlaytimeMins: playtimeMins, LastPlayed: lastPlayed,
		Providers: breakdown,
	})
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// stamp renders an instant the way the archive stores dates.
func stamp(t time.Time) string {
	return t.In(siteLocation).Format(time.RFC3339)
}

// FetchRARecord builds a provider record from RetroAchievements.
func FetchRARecord(ctx context.Context, client *RAClient, gameID string) (*ProviderRecord, error) {
	rec := &ProviderRecord{ID: gameID, LastAttempt: today()}

	progress, err := client.GetGameProgress(ctx, gameID)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}

	rec.Fetched = today()
	rec.Platform = progress.ConsoleName
	rec.Icon = progress.ImageIcon
	rec.Completion = progress.UserCompletion
	rec.Total = progress.NumAchievements
	rec.AwardKind = progress.HighestAwardKind
	rec.PlaytimeMins = progress.UserTotalPlaytime / 60
	rec.Raw = progress.Raw
	if t, err := parseRAAwardDate(progress.HighestAwardDate); err == nil {
		rec.AwardDate = day(t)
	}

	for id, a := range progress.Achievements {
		key := id
		if a.ID != 0 {
			key = strconv.Itoa(a.ID)
		}
		entry := ArchivedAchievement{
			Key:         key,
			Name:        a.Title,
			Description: a.Description,
			Points:      a.Points,
			Icon:        a.BadgeName,
		}
		if earned, hardcore, ok := a.Earned(); ok {
			entry.Unlocked = true
			entry.Date = stamp(earned)
			entry.Hardcore = hardcore
		}
		rec.Achievements = append(rec.Achievements, entry)
	}
	rec.summarize()
	return rec, nil
}

// FetchSteamRecord builds a provider record from Steam. owned may be nil; it
// supplies playtime, which the achievements endpoint doesn't carry.
func FetchSteamRecord(ctx context.Context, client *SteamClient, appID string, owned *SteamOwnedGame) (*ProviderRecord, error) {
	rec := &ProviderRecord{ID: appID, LastAttempt: today(), Platform: "PC"}

	if owned != nil {
		rec.PlaytimeMins = owned.PlaytimeMins
		rec.Icon = owned.IconURL
		rec.Playtime = map[string]int{}
		for name, mins := range map[string]int{
			"windows":      owned.PlaytimeWindows,
			"mac":          owned.PlaytimeMac,
			"linux":        owned.PlaytimeLinux,
			"deck":         owned.PlaytimeDeck,
			"disconnected": owned.PlaytimeDisconnected,
		} {
			if mins > 0 {
				rec.Playtime[name] = mins
			}
		}
		if owned.LastPlayed > 0 {
			rec.LastPlayed = day(time.Unix(owned.LastPlayed, 0))
		}
	}

	res, err := client.GetPlayerAchievements(ctx, appID)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}
	rec.Raw = res.Raw
	if !res.Success {
		rec.LastError = firstNonEmpty(res.Error, "achievement data unavailable")
		return rec, nil
	}

	rec.Fetched = today()
	rec.Total = len(res.Achievements)
	for _, a := range res.Achievements {
		entry := ArchivedAchievement{
			Key:         a.APIName,
			Name:        a.DisplayName(),
			Description: a.Description,
		}
		if a.Achieved == 1 && a.UnlockTime > 0 {
			entry.Unlocked = true
			entry.Date = stamp(time.Unix(a.UnlockTime, 0))
		}
		rec.Achievements = append(rec.Achievements, entry)
	}
	rec.summarize()
	if rec.Total > 0 && rec.Unlocked == rec.Total {
		rec.AwardKind = "all achievements"
	}
	return rec, nil
}

// savedRecord is one provider's outcome from a fetch-and-archive run.
type savedRecord struct {
	Provider string
	ID       string
	Path     string
	Record   *ArchiveRecord
}

// saveAchievements fetches from every configured provider the game is linked
// to — not just the first — and merges each result into its own archive file.
func saveAchievements(ctx context.Context, archiveDir, title string, links []providerLink, creds Credentials) ([]savedRecord, error) {
	fresh := map[string]*ProviderRecord{}
	ids := map[string]string{}

	byProvider := map[string]string{}
	for _, l := range links {
		if l.ID != "" {
			byProvider[l.Provider] = l.ID
		}
	}
	raID, steamAppID, psnID := byProvider[providerRA], byProvider[providerSteam], byProvider[providerPSN]

	if raID != "" && creds.RAConfigured() {
		client := &RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
		rec, err := FetchRARecord(ctx, client, raID)
		if err != nil {
			return nil, err
		}
		fresh[providerRA], ids[providerRA] = rec, raID
	}
	if steamAppID != "" && creds.SteamConfigured() {
		client := &SteamClient{APIKey: creds.SteamAPIKey, SteamID: creds.SteamID}
		var owned *SteamOwnedGame
		if games, err := client.GetOwnedGames(ctx); err == nil {
			for i := range games {
				if strconv.Itoa(games[i].AppID) == steamAppID {
					owned = &games[i]
					break
				}
			}
		}
		rec, err := FetchSteamRecord(ctx, client, steamAppID, owned)
		if err != nil {
			return nil, err
		}
		fresh[providerSteam], ids[providerSteam] = rec, steamAppID
	}
	if psnID != "" && creds.ExophaseConfigured() {
		client := &ExophaseClient{User: creds.ExophaseUser}
		rec, err := FetchPSNRecord(ctx, client, psnID)
		if err != nil {
			return nil, err
		}
		fresh[providerPSN], ids[providerPSN] = rec, psnID
	}

	if len(fresh) == 0 {
		return nil, fmt.Errorf("no configured provider for this game")
	}

	var saved []savedRecord
	for _, provider := range providerOrder {
		rec, ok := fresh[provider]
		if !ok {
			continue
		}
		path, err := SaveRecord(archiveDir, provider, ids[provider], title, rec)
		if err != nil {
			return nil, err
		}
		stored, err := LoadRecord(archiveDir, provider, ids[provider])
		if err != nil {
			return nil, err
		}
		saved = append(saved, savedRecord{Provider: provider, ID: ids[provider], Path: path, Record: stored})
	}
	return saved, nil
}

// runAchievements fetches (or refreshes) the achievement history for one game
// that already exists in content/games.
func runAchievements(args []string) error {
	if len(args) > 0 && args[0] == "--all" {
		return runAchievementsAll()
	}

	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}
	games, err := ListGames(gamesDir)
	if err != nil {
		return err
	}

	slug, err := resolveSuggestSlug(args, games)
	if err != nil {
		return err
	}
	var summary GameSummary
	found := false
	for _, g := range games {
		if g.Slug == slug {
			summary, found = g, true
			break
		}
	}
	if !found {
		return fmt.Errorf("no game %q found. Known games: %s", slug, joinSlugs(games))
	}

	doc, err := LoadDoc(summary.Path)
	if err != nil {
		return err
	}
	links := doc.ProviderLinks()
	if len(links) == 0 {
		return fmt.Errorf("%s has no retroachievements_id, steam_appid or psn_id set", slug)
	}

	archiveDir := findArchiveDir(gamesDir)
	saved, err := saveAchievements(context.Background(), archiveDir, summary.Title,
		links, loadCredentials())
	if err != nil {
		return err
	}

	for _, s := range saved {
		p := s.Record
		if p.LastError != "" {
			fmt.Fprintf(os.Stderr, "  %s: %s (existing data kept)\n", s.Provider, p.LastError)
		} else {
			fmt.Printf("  %s: %d/%d unlocked, %s to %s\n", s.Provider, p.Unlocked, p.Total, p.First, p.Last)
		}
		fmt.Printf("Saved %s\n", s.Path)
	}

	if _, err := writeAchievementSummary(archiveDir, filepath.Dir(summary.Path), links); err != nil {
		fmt.Fprintf(os.Stderr, "  (achievement summary not updated: %v)\n", err)
	}

	if isOneShot(doc.FM.Status) {
		if err := maybePromptNewSession(filepath.Dir(summary.Path), summary.Title, saved); err != nil {
			fmt.Fprintf(os.Stderr, "  (session check skipped: %v)\n", err)
		}
	}
	return nil
}

// maybePromptNewSession closes the gap this command otherwise leaves open:
// the timeline's visible bars come only from playthroughs.yaml, which
// refreshing provider data never touches on its own, so a burst of new play
// on a one-shot game (session-based/multiplayer/software — the statuses
// whose entire record *is* its sessions list) can sit invisible until
// someone remembers to log a session by hand. If this refresh pulled in
// activity past what's already logged, offer to log it right now instead.
//
// Only handles the unambiguous case: exactly one playthrough entry. A game
// with none yet, or more than one (a rare multi-platform one-shot), is left
// to "Log a new session" by hand rather than guessing which entry a new
// session belongs to.
func maybePromptNewSession(gameDir, title string, saved []savedRecord) error {
	pf, err := LoadPlaythroughs(gameDir)
	if err != nil {
		return err
	}
	views := pf.Views()
	if len(views) != 1 {
		return nil
	}

	var latest string
	for _, s := range saved {
		for _, d := range []string{s.Record.LastPlayed, s.Record.Last} {
			if d > latest {
				latest = d
			}
		}
	}
	known := views[0].LatestDate()
	if latest == "" || latest <= known {
		return nil
	}

	var log bool
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("%s: activity through %s isn't logged yet (last session %s). Log a session now?",
					title, latest, orDash(known))).
				Value(&log),
		),
	).Run(); err != nil {
		return err
	}
	if !log {
		return nil
	}

	started, finished, sessTitle, err := SessionForm(latest, latest)
	if err != nil {
		return err
	}
	return addSessionAndWrite(pf, 0, views[0].HasSessions(), started, finished, sessTitle)
}

// runAchievementsAll refreshes every game that has a provider link, in the
// order ListGames returns them. It's the entry point a scheduled, unattended
// run would call instead of naming one slug at a time — the loop is nothing
// more than saveAchievements/writeAchievementSummary called per game, so it
// inherits the same merge-only, never-lose-data guarantees as the one-game
// path rather than needing its own.
func runAchievementsAll() error {
	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}
	games, err := ListGames(gamesDir)
	if err != nil {
		return err
	}
	archiveDir := findArchiveDir(gamesDir)
	creds := loadCredentials()

	var refreshed, skipped, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			skipped++
			continue
		}

		fmt.Println(g.Title)
		saved, err := saveAchievements(context.Background(), archiveDir, g.Title, links, creds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %v\n", err)
			failed++
			continue
		}
		for _, s := range saved {
			p := s.Record
			if p.LastError != "" {
				fmt.Fprintf(os.Stderr, "  %s: %s (existing data kept)\n", s.Provider, p.LastError)
			} else {
				fmt.Printf("  %s: %d/%d unlocked, %s to %s\n", s.Provider, p.Unlocked, p.Total, p.First, p.Last)
			}
		}
		if _, err := writeAchievementSummary(archiveDir, filepath.Dir(g.Path), links); err != nil {
			fmt.Fprintf(os.Stderr, "  (achievement summary not updated: %v)\n", err)
		}
		refreshed++
	}
	fmt.Printf("%d refreshed, %d with no provider link, %d failed\n", refreshed, skipped, failed)
	return nil
}

// runProject regenerates every game's achievement-summary.yaml purely from
// what's already archived — no API calls, so it's safe and fast to run any
// time the projection needs rebuilding (after a manual archive edit, or a
// change to what the summary contains) without re-fetching anything.
func runProject(args []string) error {
	gamesDir, err := findGamesDir()
	if err != nil {
		return err
	}
	games, err := ListGames(gamesDir)
	if err != nil {
		return err
	}
	archiveDir := findArchiveDir(gamesDir)

	var written, empty, failed int
	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			continue
		}
		wrote, err := writeAchievementSummary(archiveDir, filepath.Dir(g.Path), links)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "  %s: %v\n", g.Slug, err)
			failed++
		case wrote:
			written++
		default:
			empty++
		}
	}
	fmt.Printf("%d summaries written, %d with nothing archived yet, %d failed\n", written, empty, failed)
	return nil
}
