package model

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SiteLocation is the timezone dates are reported in. RA returns UTC and
// Steam returns Unix timestamps rendered in system-local time; without
// normalising, an evening session can be reported a day late. This matches
// `timezone` in config/_default/hugo.toml, so suggested dates line up with
// the dates the site itself renders.
var SiteLocation = func() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.Local
	}
	return loc
}()

// FirstNonEmpty returns the first non-empty string among values.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

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
	ProviderRA      = "retroachievements"
	ProviderSteam   = "steam"
	ProviderPSN     = "psn"
	ProviderUbisoft = "ubisoft"

	// ProviderXbox comes from OpenXBL (https://xbl.io), an unofficial Xbox
	// Live API — Microsoft has no public first-party one. Like Steam's
	// GetPlayerAchievements, the x360-specific achievement endpoint this
	// uses only reports achievements that are actually unlocked; the total
	// possible count comes from a separate title-history call instead (see
	// providers/xbox). A handful of the earliest records here were seeded
	// from a one-time pasted export before the live client existed, and are
	// summary-only (Unlocked/Total set directly, Achievements empty) —
	// refreshing them through the live client backfills real per-achievement
	// data and merges normally from that point on.
	ProviderXbox = "xbox"

	// ProviderGooglePlay is Google Play Games, mirrored through Exophase like
	// ProviderUbisoft — Google's own Play Games API only answers per app, for
	// the app's own OAuth client, so there is no first-party way to list one
	// player's achievements across games. The id is the Play Games
	// application id (a 12-digit number), Google's own and permanent.
	ProviderGooglePlay = "googleplay"
)

// ProviderInfo is everything the rest of the tool needs to know about one
// achievement provider, kept in one table (providerTable) so adding a provider
// is one row here, not a hunt for parallel lists. The site's own list is
// data/providers.yaml; TestProvidersYAMLMatchesTable keeps the two in step.
type ProviderInfo struct {
	Key   string // archive key and `provider:` value, e.g. "steam"
	Label string // the name a reader knows it by
	// FMKey is the front-matter field holding a game's id on this provider.
	FMKey string
	// FormLabel and Placeholder label this provider's input in the provider-IDs
	// form. NumericID means the field takes a bare number or the URL it came
	// from (externalid.ParseExternalID); the rest are free text.
	FormLabel   string
	Placeholder string
	NumericID   bool
}

// providerTable lists the achievement providers in ProviderOrder. Nadeo and
// Ninja Kiwi are absent on purpose — see ProviderOrder.
var providerTable = []ProviderInfo{
	{Key: ProviderRA, Label: "RetroAchievements", FMKey: "retroachievements_id",
		FormLabel: "RetroAchievements ID", Placeholder: "numeric ID or game URL", NumericID: true},
	{Key: ProviderSteam, Label: "Steam", FMKey: "steam_appid",
		FormLabel: "Steam appid", Placeholder: "numeric appid or store URL", NumericID: true},
	{Key: ProviderPSN, Label: "PSN", FMKey: "psn_id",
		FormLabel: "PSN trophy-set ID", Placeholder: "NPWR…"},
	{Key: ProviderUbisoft, Label: "Ubisoft", FMKey: "ubisoft_id",
		FormLabel: "Ubisoft (Exophase) ID"},
	{Key: ProviderXbox, Label: "Xbox", FMKey: "xbox_id",
		FormLabel: "Xbox title ID"},
	{Key: ProviderGooglePlay, Label: "Google Play", FMKey: "googleplay_id",
		FormLabel: "Google Play app ID", Placeholder: "package name, e.g. com.example.game"},
}

// Providers returns the provider table in ProviderOrder.
func Providers() []ProviderInfo { return append([]ProviderInfo(nil), providerTable...) }

func providerInfo(provider string) (ProviderInfo, bool) {
	for _, p := range providerTable {
		if p.Key == provider {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// ProviderLabel names a provider for display, falling back to its key.
func ProviderLabel(provider string) string {
	if p, ok := providerInfo(provider); ok {
		return p.Label
	}
	return provider
}

// ProviderLink pairs a provider with this game's ID on it. Everything that
// walks a game's archive takes a slice of these rather than a positional
// (raID, steamAppID) pair, so adding a provider doesn't mean editing every
// signature that touches the archive.
type ProviderLink struct {
	Provider string
	ID       string

	// Subset marks a RetroAchievements subset — a bonus achievement set RA
	// publishes as its own game id under the base game's roof (see
	// FrontMatter.RASubsets). It's still a normal archive record fetched the
	// normal way; the flag exists so the achievement summary can keep a
	// subset's 52 achievements out of the base game's headline denominator
	// while still capturing them.
	Subset bool
}

// ProviderOrder fixes the order records are written and reported in, so
// output and tests don't depend on map iteration.
//
// ProviderNadeo is absent on purpose — it records lap times, not achievements,
// and everything driven by this list assumes an Unlocked/Total pair it can sum.
// Adding it here would fold ~625 Trackmania campaign tracks into that game's
// achievement denominator. See the ProviderNadeo doc comment in nadeo.go.
var ProviderOrder = func() []string {
	keys := make([]string, len(providerTable))
	for i, p := range providerTable {
		keys[i] = p.Key
	}
	return keys
}()

// raSubsetTitle matches RetroAchievements' subset naming convention, which is
// the *only* thing distinguishing a subset in the bulk endpoints — e.g.
// "Professor Layton and the Last Specter [Subset - Mouse Alley]". The
// per-game endpoint reports a real ParentGameID; this is what lets the scan
// know which of its rows are worth spending that request on.
var raSubsetTitle = regexp.MustCompile(`(?i)^(.*?)\s*\[Subset\s*-\s*(.+?)\]\s*$`)

// SplitRASubsetTitle splits a RetroAchievements subset title into the base
// game's name and the subset's own, reporting whether it was one at all.
// Matching on the title alone is a heuristic, so it is never used to *link* a
// subset — only to spot a candidate and to label one already linked by id.
func SplitRASubsetTitle(title string) (base, subset string, ok bool) {
	m := raSubsetTitle.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true
}

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
	// Tier is PSN's trophy grade — bronze/silver/gold/platinum. Left empty by
	// every other provider; no equivalent concept exists for RA/Steam/Xbox.
	Tier string `json:"tier,omitempty"`
	// Hidden marks an achievement whose name/description the provider
	// withholds until earned, to avoid spoiling it. Steam and PSN both
	// report this; RA/Xbox/Exophase have no equivalent concept.
	Hidden bool `json:"hidden,omitempty"`
	// Group is PSN's trophyGroupId: "default" for the base game, "001",
	// "002", … for each DLC pack. It's what lets base-game and DLC mastery be
	// dated separately. No other provider reports anything like it.
	Group string `json:"group,omitempty"`
	// Rarity is the percentage (0-100) of the provider's players who have
	// earned this achievement, as of the fetch that last reported it. It
	// drifts over time, so a fresh non-zero value replaces the old one; zero
	// means "not reported", never "nobody has it". Every provider reports it;
	// Exophase's figure is over its own players who track the game, not the
	// service's whole player base.
	Rarity float64 `json:"rarity,omitempty"`
}

// ArchivedStat is one per-game stat a provider tracks for the player — Steam's
// "furlongs travelled", "crops shipped", Team Fortress 2's per-class damage.
// These are counters and gauges, not unlocks, so they sit beside Achievements
// rather than in it, and no achievement count is derived from them.
type ArchivedStat struct {
	Key  string `json:"key"`            // provider-stable id: Steam's stat name
	Name string `json:"name,omitempty"` // the game's display name; often blank, in which case Key is what there is
	// Value is nil when the game defines the stat but the provider reported
	// nothing for the player. Steam omits stats still at their default, so
	// nil is "not reported", never "zero".
	Value   *float64 `json:"value,omitempty"`
	Default float64  `json:"default,omitempty"`
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

	// ProviderTitle is the provider's own name for what was just fetched. Not
	// persisted here — ArchiveRecord.Title is where a title lands — it only
	// carries the name from a fetch to SaveRecord, for the one case where the
	// caller doesn't have it: a RetroAchievements subset, whose name ("Mouse
	// Alley") exists nowhere in this repo except the record itself.
	ProviderTitle string `json:"-"`

	// Source names where the data came from when that isn't the provider's
	// own API — "exophase" for the PlayStation mirror. Without it a later run
	// against Sony's endpoints couldn't tell first-hand data from relayed
	// data, and the merge rules would have no way to prefer the former.
	Source string `json:"source,omitempty"`

	PlaytimeMins int            `json:"playtime_mins,omitempty"`
	Playtime     map[string]int `json:"playtime,omitempty"` // per-device breakdown
	LastPlayed   string         `json:"last_played,omitempty"`

	Achievements []ArchivedAchievement `json:"achievements"`

	// Stats is every per-game stat the provider has reported or defined for
	// this game. Merged by key and never shrunk — see mergeStats.
	Stats []ArchivedStat `json:"stats,omitempty"`

	// Mastery is every mastery milestone ever observed on this record. It is
	// persisted rather than only derived because it can stop being derivable:
	// a DLC that adds achievements makes an old 100% read as 54/60, and an RA
	// set revision can demote HighestAwardKind. See DeriveMastery and
	// unionMastery — milestones are only ever added.
	Mastery []MasteryMilestone `json:"mastery,omitempty"`

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
func (p *ProviderRecord) Summarize() {
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
	p.Mastery = unionMastery(p.Mastery, DeriveMastery(p))
}

// mergeProvider folds a fresh fetch into what's already recorded. The rule
// that matters: **a refresh must never be able to lose data.** An unlock
// recorded once stays recorded even if the provider later denies it —
// profiles go private, games get delisted, rate limits return empty bodies,
// and none of those should erase history.
func mergeProvider(old, fresh *ProviderRecord) *ProviderRecord {
	if old == nil {
		fresh.Summarize()
		return fresh
	}
	if fresh == nil {
		return old
	}

	merged := *old
	// Fold in whatever the old record already qualified for before anything
	// changes, so a record written before milestones existed can't lose one
	// to the very fetch that adds DLC achievements.
	merged.Mastery = unionMastery(old.Mastery, DeriveMastery(old))
	merged.LastAttempt = fresh.LastAttempt
	merged.LastError = fresh.LastError
	mergeActivity(&merged, fresh)

	// A fetch whose achievement half produced nothing usable updates only the
	// attempt metadata and the activity fields above; the achievement data is
	// left entirely alone. A successful empty fetch (Steam's "no stats") still
	// advances Fetched, so a bulk refresh can tell it's current.
	if fresh.LastError != "" || len(fresh.Achievements) == 0 {
		if fresh.ID != "" {
			merged.ID = fresh.ID
		}
		if fresh.LastError == "" && fresh.Fetched != "" {
			merged.Fetched = fresh.Fetched
		}
		return &merged
	}

	merged.Fetched = fresh.Fetched
	merged.ID = FirstNonEmpty(fresh.ID, old.ID)
	// Deliberately assigned, not FirstNonEmpty: a first-party fetch reports
	// Source "" and must be able to clear an earlier "exophase", otherwise a
	// record would claim to be relayed forever after a first-party refresh.
	merged.Source = fresh.Source
	merged.AwardKind = FirstNonEmpty(fresh.AwardKind, old.AwardKind)
	merged.AwardDate = FirstNonEmpty(fresh.AwardDate, old.AwardDate)
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
	merged.Mastery = unionMastery(merged.Mastery, fresh.Mastery)
	merged.Summarize()
	return &merged
}

// mergeActivity folds in the fields that describe the game rather than its
// achievement list: playtime, last played, the declared total, platform, icon,
// completion. Providers read these from a listing separate from the
// achievement fetch (Steam's owned games, Xbox title history, PSN's and
// Exophase's title lists, RA's recently played), so they are real even when
// the achievement half failed or the game has no achievements at all. Every
// rule here can only add: counts and dates never go backwards, a blank never
// replaces a value.
func mergeActivity(merged, fresh *ProviderRecord) {
	merged.Platform = FirstNonEmpty(fresh.Platform, merged.Platform)
	merged.Icon = FirstNonEmpty(fresh.Icon, merged.Icon)
	merged.Completion = FirstNonEmpty(fresh.Completion, merged.Completion)
	if fresh.LastPlayed > merged.LastPlayed {
		merged.LastPlayed = fresh.LastPlayed
	}
	if fresh.Total > merged.Total {
		merged.Total = fresh.Total
	}
	if fresh.PlaytimeMins > merged.PlaytimeMins {
		merged.PlaytimeMins = fresh.PlaytimeMins
	}
	merged.Playtime = mergeMaxIntMap(merged.Playtime, fresh.Playtime)
	merged.Stats = mergeStats(merged.Stats, fresh.Stats)
}

// mergeStats unions stats by key, keeping the old order and appending new
// keys. It lives with the activity fields rather than the achievement merge
// because a game can have stats and no achievements at all, and a fetch whose
// achievement half failed can still have read them.
//
// A reported value is taken from the fresh fetch even when it is lower: unlike
// achievements, a stat is a live reading that games reset, rescale or spend,
// and Steam's current answer is the truth about it. What a refresh can never
// do is drop a stat or blank its value — a key the fetch didn't mention, or
// mentioned without a value, keeps what was captured.
func mergeStats(old, fresh []ArchivedStat) []ArchivedStat {
	if len(fresh) == 0 {
		return old
	}
	byKey := make(map[string]ArchivedStat, len(old)+len(fresh))
	order := make([]string, 0, len(old)+len(fresh))
	for _, s := range old {
		if _, seen := byKey[s.Key]; !seen {
			order = append(order, s.Key)
		}
		byKey[s.Key] = s
	}
	for _, f := range fresh {
		prev, seen := byKey[f.Key]
		if !seen {
			order = append(order, f.Key)
			byKey[f.Key] = f
			continue
		}
		out := f
		out.Name = FirstNonEmpty(f.Name, prev.Name)
		if f.Value == nil {
			out.Value = prev.Value
		}
		if f.Default == 0 {
			out.Default = prev.Default
		}
		byKey[f.Key] = out
	}
	merged := make([]ArchivedStat, 0, len(order))
	for _, k := range order {
		merged = append(merged, byKey[k])
	}
	return merged
}

// mergeAchievement keeps the union of what's known. An unlock, once seen, is
// never retracted, and its original date is preferred over a later one.
func mergeAchievement(old, fresh ArchivedAchievement) ArchivedAchievement {
	out := fresh
	out.Name = FirstNonEmpty(fresh.Name, old.Name)
	// Name and description travel together: a fresh entry with no description
	// is a provider that didn't (or couldn't) say more this time — Exophase's
	// page fetch failing leaves only a slug as the name — and it must not
	// replace the real name an earlier refresh captured.
	if fresh.Description == "" && old.Description != "" {
		out.Name = old.Name
	}
	out.Description = FirstNonEmpty(fresh.Description, old.Description)
	out.Icon = FirstNonEmpty(fresh.Icon, old.Icon)
	out.Tier = FirstNonEmpty(fresh.Tier, old.Tier)
	out.Group = FirstNonEmpty(fresh.Group, old.Group)
	out.Hidden = fresh.Hidden || old.Hidden
	if fresh.Rarity == 0 {
		out.Rarity = old.Rarity
	}
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
		out.Date = FirstNonEmpty(old.Date, fresh.Date)
		out.Hardcore = old.Hardcore || fresh.Hardcore
	}
	return out
}

// RecordPath is where one provider's history for one game lives.
func RecordPath(archiveDir, provider, id string) string {
	return filepath.Join(archiveDir, provider, id+".json")
}

// FindArchiveDir locates the archive next to content/, deriving the repo root
// from where content/games was found rather than searching again.
func FindArchiveDir(gamesDir string) string {
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
	rec.Updated = time.Now().In(SiteLocation).Format("2006-01-02")

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
// most recent activity date across every linked record.
type AchievementSummary struct {
	Unlocked     int    `yaml:"unlocked"`
	Total        int    `yaml:"total"`
	PlaytimeMins int    `yaml:"playtime_mins,omitempty"`
	LastPlayed   string `yaml:"last_played,omitempty"`

	// Mastered is the day the game first counted as mastered on any provider
	// it's played on (subsets excluded): the earliest base-game or full-set
	// milestone in Providers[].Mastery. Empty means not mastered.
	Mastered string `yaml:"mastered,omitempty"`

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

	// Subsets is every RetroAchievements subset linked to this game, each with
	// its own counts, kept out of the totals above on purpose. A subset is a
	// bonus achievement set for the same game, not a second copy of it: adding
	// Mouse Alley's 52 to Last Specter's 40 would restate the game as 24%
	// complete when the game itself is 40% complete, and there is no set
	// anywhere that has 92 achievements in it.
	Subsets []SubsetBreakdown `yaml:"subsets,omitempty"`

	// Earned is every unlocked achievement across every linked provider, kept
	// here (rather than read from archive/ directly) for the same reason the
	// rest of this projection exists: Hugo never reads archive/ or its `raw`
	// payloads at build time. Sorted oldest-first, matching summarize()'s
	// convention on ProviderRecord.Achievements — newest-first is a display
	// choice, made by whatever reads this file.
	Earned []EarnedAchievement `yaml:"earned,omitempty"`
}

// EarnedAchievement is one unlocked achievement, flattened out of a game's
// provider records for display. Locked achievements aren't included here —
// this is a "what did I earn" projection, not a duplicate of the archive.
type EarnedAchievement struct {
	// ID is the provider record (archive/<provider>/<id>.json) and Key the
	// achievement's provider-stable id within it: together the address a
	// curated list uses, since names can be rewritten by a provider.
	ID          string `yaml:"id,omitempty"`
	Key         string `yaml:"key,omitempty"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Date        string `yaml:"date"`
	Points      int    `yaml:"points,omitempty"`
	Hardcore    bool   `yaml:"hardcore,omitempty"`
	Icon        string `yaml:"icon,omitempty"`
	Provider    string `yaml:"provider"`
	Platform    string `yaml:"platform,omitempty"`
	Hidden      bool   `yaml:"hidden,omitempty"`

	// Rarity is the percent of players who earned it; omitted when the
	// provider didn't report one.
	Rarity float64 `yaml:"rarity,omitempty"`

	// Subset names the RetroAchievements subset this one came from, blank for
	// the base set. Subset unlocks stay in this one flat list rather than
	// hiding inside SubsetBreakdown: they were genuinely earned, and the
	// site-wide achievement list would otherwise silently omit them. The tag
	// is what lets a view group or label them without a second source.
	Subset string `yaml:"subset,omitempty"`
}

// SubsetBreakdown is one RetroAchievements subset's contribution to a game —
// deliberately not a ProviderBreakdown, because it is not a second place the
// game was played and must not be summed with one.
type SubsetBreakdown struct {
	// Name is the subset's own half of RA's "Base [Subset - Name]" title,
	// falling back to the whole title if it doesn't follow the convention.
	// It's derived from the archived record rather than stored in front
	// matter, for the same reason ArchiveRecord.Title is the provider's own
	// name for a game: the provider owns what its set is called.
	Name     string `yaml:"name"`
	Provider string `yaml:"provider"`
	ID       string `yaml:"id"`
	Platform string `yaml:"platform,omitempty"`
	Unlocked int    `yaml:"unlocked"`
	Total    int    `yaml:"total"`
	// PlaytimeMins is reported per RA game id, and a subset is the same game
	// played again under a different id — so it is kept here and deliberately
	// left out of the game's total, which would otherwise double-count the
	// same hours.
	PlaytimeMins int                `yaml:"playtime_mins,omitempty"`
	LastPlayed   string             `yaml:"last_played,omitempty"`
	Mastery      []MasteryMilestone `yaml:"mastery,omitempty"`
}

// ProviderBreakdown is one provider's contribution to a game's summary.
type ProviderBreakdown struct {
	Provider string `yaml:"provider"`
	// Platform is the provider's own name for where it was played (PS4, PC,
	// GameCube). It's what a reader actually recognises — "psn" is plumbing.
	Platform     string             `yaml:"platform,omitempty"`
	Unlocked     int                `yaml:"unlocked"`
	Total        int                `yaml:"total"`
	PlaytimeMins int                `yaml:"playtime_mins,omitempty"`
	LastPlayed   string             `yaml:"last_played,omitempty"`
	Mastery      []MasteryMilestone `yaml:"mastery,omitempty"`
}

func achievementSummaryPath(gameDir string) string {
	return filepath.Join(gameDir, achievementSummaryFilename)
}

// WriteAchievementSummary recomputes the projection from whatever is
// currently archived for either provider, not just a record just fetched.
// A game with nothing archived gets no file; a stale one is removed. wrote
// reports whether a file now exists, so callers can tally writes vs. no-ops.
func WriteAchievementSummary(archiveDir, gameDir string, links []ProviderLink) (wrote bool, err error) {
	// Stats are a separate projection but ride along on every call, so each
	// path that already refreshes a summary keeps stats-summary.yaml current.
	if err := writeStatsSummary(archiveDir, gameDir, links); err != nil {
		return false, err
	}

	var unlocked, total, playtimeMins int
	var lastPlayed string
	var breakdown []ProviderBreakdown
	var subsets []SubsetBreakdown
	var earned []EarnedAchievement
	var mastered string
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
		// Derived as well as read back, so a record not refreshed since
		// milestones were introduced still projects what it qualifies for.
		mastery := unionMastery(rec.Mastery, DeriveMastery(&rec.ProviderRecord))
		subsetName := ""
		if link.Subset {
			// A subset gets its own line and contributes nothing to the
			// headline counts or the provider breakdown — see
			// AchievementSummary.Subsets for why summing them is wrong.
			// RA's own title for the set, reduced to just the subset's half.
			// A record captured before its title was known falls back to the
			// id, so the line still identifies which set it is.
			subsetName = FirstNonEmpty(rec.Title, id)
			if _, name, ok := SplitRASubsetTitle(rec.Title); ok {
				subsetName = name
			}
			subsets = append(subsets, SubsetBreakdown{
				Name:         subsetName,
				Provider:     provider,
				ID:           id,
				Platform:     rec.Platform,
				Unlocked:     rec.Unlocked,
				Total:        rec.Total,
				PlaytimeMins: rec.PlaytimeMins,
				LastPlayed:   FirstNonEmpty(rec.LastPlayed, rec.Last),
				Mastery:      mastery,
			})
		} else {
			breakdown = append(breakdown, ProviderBreakdown{
				Provider:     provider,
				Platform:     rec.Platform,
				Unlocked:     rec.Unlocked,
				Total:        rec.Total,
				PlaytimeMins: rec.PlaytimeMins,
				LastPlayed:   FirstNonEmpty(rec.LastPlayed, rec.Last),
				Mastery:      mastery,
			})
			if d := MasteredOn(mastery); d != "" && (mastered == "" || d < mastered) {
				mastered = d
			}
			unlocked += rec.Unlocked
			total += rec.Total
			playtimeMins += rec.PlaytimeMins
		}
		for _, a := range rec.Achievements {
			if !a.Unlocked {
				continue
			}
			earned = append(earned, EarnedAchievement{
				ID:          id,
				Key:         a.Key,
				Name:        a.Name,
				Description: a.Description,
				Date:        a.Date,
				Points:      a.Points,
				Hardcore:    a.Hardcore,
				Icon:        a.Icon,
				Provider:    provider,
				Platform:    rec.Platform,
				Hidden:      a.Hidden,
				Rarity:      summaryRarity(a.Rarity),
				Subset:      subsetName,
			})
		}
		// A subset's dates are folded in with everything else: playing the
		// bonus set is playing the game, and hiding that would make the site's
		// "last active" older than the archive knows it to be.
		//
		// LastPlayed is the provider's own "last played" (Steam's
		// rtime_last_played, RA's GetUserRecentlyPlayedGames, PSN's) when
		// it reports one. Last, the newest unlock, covers a record without
		// it, and wins when it's later.
		for _, d := range []string{rec.LastPlayed, rec.Last} {
			if d != "" && d > lastPlayed {
				lastPlayed = d
			}
		}
	}

	path := achievementSummaryPath(gameDir)
	if total == 0 && playtimeMins == 0 && lastPlayed == "" && len(subsets) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}

	sort.Slice(earned, func(i, j int) bool { return earned[i].Date < earned[j].Date })

	out, err := yaml.Marshal(AchievementSummary{
		Unlocked: unlocked, Total: total, PlaytimeMins: playtimeMins, LastPlayed: lastPlayed,
		Mastered:  mastered,
		Providers: breakdown, Subsets: subsets, Earned: earned,
	})
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// LoadAchievementSummary reads back the projection WriteAchievementSummary
// wrote, returning nil (not an error) when the game has no achievements
// captured yet.
func LoadAchievementSummary(gameDir string) (*AchievementSummary, error) {
	path := achievementSummaryPath(gameDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s AchievementSummary
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// summaryRarity rounds to the two decimals the site shows, so drift doesn't rewrite the summary; a nonzero value never rounds away.
func summaryRarity(r float64) float64 {
	if r <= 0 {
		return 0
	}
	return math.Max(math.Round(r*100)/100, 0.01)
}
