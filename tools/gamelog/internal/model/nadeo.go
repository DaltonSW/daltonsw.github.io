package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// ProviderNadeo is Trackmania (2020), captured from Nadeo's own web services.
//
// It is deliberately *not* in ProviderOrder, and deliberately not produced by
// BuildProviderLinks. Every other provider records achievements, and everything
// that walks ProviderLinks — SaveAchievements, WriteAchievementSummary,
// HasArchiveRecord — assumes an ArchiveRecord with an Unlocked/Total pair it
// can sum. Nadeo records per-track *times*, which are neither.
//
// Folding them in would do real damage rather than just reading oddly:
// Trackmania's headline meter is 21/30 from its Ubisoft record, and adding ~450
// campaign tracks to that denominator would restate the game as 433/480 of an
// achievement set that doesn't exist. And mergeProvider merges numbers upward
// (`if fresh.Total > merged.Total`), which is exactly backwards for a lap time.
//
// So Nadeo gets its own record type, its own merge, and its own projection,
// below. KNOWN-ISSUES.md already carries this decision for Nintendo — a
// provider whose data isn't achievements needs a shape that doesn't pretend
// otherwise.
const ProviderNadeo = "nadeo"

// NadeoRecord is one account's captured Trackmania campaign history. Unlike
// ArchiveRecord it is keyed by *account* id rather than a per-game id: Nadeo's
// API covers exactly one game, so "which game" is never in question and the
// only stable identifier in play is whose history it is.
type NadeoRecord struct {
	Provider    string `json:"provider"` // always ProviderNadeo
	Title       string `json:"title"`    // the provider's name for the game
	ID          string `json:"id"`       // accountId, a UUID assigned once and never changed
	DisplayName string `json:"display_name,omitempty"`

	Updated     string `json:"updated"`                // YYYY-MM-DD, last successful write
	Fetched     string `json:"fetched,omitempty"`      // YYYY-MM-DD, last fetch that produced data
	LastAttempt string `json:"last_attempt,omitempty"` // YYYY-MM-DD, including failed ones
	LastError   string `json:"last_error,omitempty"`

	Campaigns []NadeoCampaign `json:"campaigns"`

	// Raw holds the verbatim responses behind the most recent successful fetch,
	// replaced wholesale each time — the same rule mergeProvider applies to
	// ArchiveRecord.Raw.
	//
	// It is deliberately *not* stored per campaign. Map metadata and records are
	// batched across season boundaries (150 maps and every season's records per
	// request), so there is no verbatim slice of a response that belongs to one
	// campaign; attaching the batch to each campaign it touched would store the
	// same payload once per season. That means a --season fetch narrows `raw` to
	// that season's scope. Nothing decoded is lost when it does — the merge
	// guarantees that separately — and `raw` is a safety net for a field this
	// tool doesn't model yet, which a re-fetch restores.
	Raw *NadeoRaw `json:"raw,omitempty"`
}

// NadeoRaw is the untouched provider payload behind one fetch.
type NadeoRaw struct {
	Campaigns []json.RawMessage `json:"campaigns,omitempty"`
	Maps      []json.RawMessage `json:"maps,omitempty"`
	Records   []json.RawMessage `json:"records,omitempty"`
}

// Empty reports whether this carries nothing worth storing.
func (r *NadeoRaw) Empty() bool {
	return r == nil || (len(r.Campaigns) == 0 && len(r.Maps) == 0 && len(r.Records) == 0)
}

// NadeoCampaign is one official seasonal campaign — "Fall 2024" and its 25
// tracks.
type NadeoCampaign struct {
	// SeasonUID doubles as the groupUid every leaderboard call needs. For
	// official campaigns the two are the same identifier (per Nadeo's glossary);
	// they are not for club campaigns, which this doesn't capture.
	SeasonUID  string `json:"season_uid"`
	CampaignID int    `json:"campaign_id,omitempty"`
	Name       string `json:"name"`

	// Dates are string-typed for the same reason every other date in this
	// package is: an unquoted 2024-10-01 round-trips through YAML as a
	// timestamp, and the text stops matching what the file said.
	Started string `json:"started,omitempty"`
	Ended   string `json:"ended,omitempty"`

	Tracks []NadeoTrack `json:"tracks"`
}

// NadeoTrack is one campaign map and this account's best time on it.
type NadeoTrack struct {
	Position int    `json:"position"`
	MapUID   string `json:"map_uid"`

	// MapID is a different identifier from MapUID and the two are not
	// interchangeable: the UID is generated when a map is saved in the editor,
	// the ID when it's uploaded. Leaderboard calls take UIDs; the Core record
	// endpoints take IDs. Both are captured so neither needs re-deriving.
	MapID string `json:"map_id,omitempty"`

	// Name is Nadeo's own map name, which carries in-game formatting codes
	// ($i$a1b…) on some maps. Stored exactly as returned — stripping them is a
	// display decision.
	Name         string `json:"name,omitempty"`
	AuthorID     string `json:"author_id,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`

	// Medal thresholds, in milliseconds, fastest first. These are properties of
	// the map, not of the player, and are what make Medal() computable without
	// a second fetch.
	AuthorMs int `json:"author_ms,omitempty"`
	GoldMs   int `json:"gold_ms,omitempty"`
	SilverMs int `json:"silver_ms,omitempty"`
	BronzeMs int `json:"bronze_ms,omitempty"`

	// PersonalBestMs is the all-time best on this track — including runs driven
	// long after the campaign closed, which is the number the game itself shows
	// and the one that reflects going back to clean up old medals. Zero when
	// never driven; zero is safe as a sentinel because no Trackmania time is
	// ever 0ms.
	PersonalBestMs int `json:"personal_best_ms,omitempty"`

	// SeasonBestMs is the best time set *while the campaign was open*, which
	// stops moving when the season closes. Kept alongside the all-time best
	// rather than instead of it: the gap between the two is the record of
	// having come back to a track, and neither number can be derived from the
	// other.
	//
	// It can be absent while PersonalBestMs is set (a track first driven after
	// its season ended), and the two are equal whenever a track hasn't been
	// revisited.
	SeasonBestMs   int    `json:"season_best_ms,omitempty"`
	SeasonDrivenAt string `json:"season_driven_at,omitempty"`

	// WorldRank comes from the Live leaderboard, which the Core records
	// endpoint doesn't report — so it's only populated on the fallback path.
	WorldRank int `json:"world_rank,omitempty"`

	// DrivenAt is when this run was actually set (YYYY-MM-DD), from the Core
	// records endpoint. This is the only per-track date the API offers, and the
	// reason that endpoint is preferred: it's what could eventually place
	// campaign progress on the site's timeline.
	DrivenAt string `json:"driven_at,omitempty"`

	// RecordID and ReplayURL identify the run itself. Neither is used yet;
	// they're captured because the API offers them and a replay that gets
	// beaten is gone from the leaderboard for good.
	RecordID  string `json:"record_id,omitempty"`
	ReplayURL string `json:"replay_url,omitempty"`

	// NadeoMedal is the provider's own numeric medal grade, kept alongside the
	// thresholds rather than instead of them. Medal() still derives from the
	// times, which is verifiable arithmetic; this is the provider's opinion,
	// stored so the two can be compared if they ever disagree.
	NadeoMedal int `json:"nadeo_medal,omitempty"`

	// FirstSeen is when this track first entered the archive, which is the
	// closest thing to a capture date available per-track.
	FirstSeen string `json:"first_seen,omitempty"`
}

// Medal names the best medal this time earns: author, gold, silver, bronze, or
// "none" for a time that beat no threshold. An undriven track returns "",
// which is a different thing from "none" and must stay distinguishable —
// "drove it and missed bronze" is a real result.
//
// Thresholds are inclusive: matching the author time exactly earns the author
// medal, as it does in game.
func (t NadeoTrack) Medal() string { return t.medalFor(t.PersonalBestMs) }

// SeasonMedal is the medal held when the campaign closed, which may be worse
// than Medal if the track was revisited later.
func (t NadeoTrack) SeasonMedal() string { return t.medalFor(t.SeasonBestMs) }

func (t NadeoTrack) medalFor(ms int) string {
	if ms <= 0 {
		return ""
	}
	for _, m := range []struct {
		name      string
		threshold int
	}{
		{"author", t.AuthorMs},
		{"gold", t.GoldMs},
		{"silver", t.SilverMs},
		{"bronze", t.BronzeMs},
	} {
		if m.threshold > 0 && ms <= m.threshold {
			return m.name
		}
	}
	return "none"
}

// medalRank orders medals worst to best so two can be compared. "" (never
// driven) sorts below "none" (driven, beat nothing), which is the distinction
// the whole grid rests on.
var medalRank = map[string]int{"": 0, "none": 1, "bronze": 2, "silver": 3, "gold": 4, "author": 5}

// ImprovedSinceSeason reports whether the track was revisited after its
// campaign closed and the medal actually got better — not merely a faster time,
// which happens without changing what the grid shows.
func (t NadeoTrack) ImprovedSinceSeason() bool {
	if t.SeasonBestMs == 0 || t.PersonalBestMs == 0 {
		return false
	}
	return medalRank[t.Medal()] > medalRank[t.SeasonMedal()]
}

// Driven reports whether this track has a time on it at all.
func (t NadeoTrack) Driven() bool { return t.PersonalBestMs > 0 }

// mergeNadeoRecord folds a fresh fetch into what's already archived. It carries
// the same promise as mergeProvider — **a refresh must never be able to lose
// data** — but proves it separately, because two of its rules are inverted from
// that one and the guarantee doesn't transfer:
//
//   - mergeProvider takes the *larger* of two numbers, since more achievements
//     is more history. A lap time is better when it's *smaller*, so a personal
//     best merges by minimum. Taking the max here would silently replace every
//     record with the worst time ever seen for it.
//   - A fetch may legitimately cover one season (gamelog nadeo fetch --season),
//     so "this season is missing from the response" cannot mean "delete it".
//     Campaigns and tracks are unioned by id, never replaced wholesale, which
//     is also what makes a rate-limited or partial response harmless.
func mergeNadeoRecord(old, fresh *NadeoRecord) *NadeoRecord {
	if old == nil {
		return fresh
	}
	if fresh == nil {
		return old
	}

	merged := *old
	merged.LastAttempt = fresh.LastAttempt
	merged.LastError = fresh.LastError

	// A fetch that produced nothing usable updates only the attempt metadata.
	// Rate limits, a revoked service account and a delisted season all land
	// here, and none of them should be able to erase what's already captured.
	if fresh.LastError != "" || len(fresh.Campaigns) == 0 {
		return &merged
	}

	merged.Fetched = FirstNonEmpty(fresh.Fetched, merged.Fetched)
	merged.DisplayName = FirstNonEmpty(fresh.DisplayName, merged.DisplayName)
	merged.Title = FirstNonEmpty(fresh.Title, merged.Title)
	if fresh.ID != "" {
		merged.ID = fresh.ID
	}
	if !fresh.Raw.Empty() {
		merged.Raw = fresh.Raw
	}

	// Existing order is preserved and new campaigns append, so the archive
	// stays stable across runs and diffs stay readable.
	index := make(map[string]int, len(old.Campaigns))
	campaigns := make([]NadeoCampaign, len(old.Campaigns))
	copy(campaigns, old.Campaigns)
	for i, c := range campaigns {
		index[c.SeasonUID] = i
	}
	for _, fc := range fresh.Campaigns {
		if i, ok := index[fc.SeasonUID]; ok {
			campaigns[i] = mergeNadeoCampaign(campaigns[i], fc)
			continue
		}
		index[fc.SeasonUID] = len(campaigns)
		campaigns = append(campaigns, fc)
	}
	merged.Campaigns = campaigns
	return &merged
}

func mergeNadeoCampaign(old, fresh NadeoCampaign) NadeoCampaign {
	out := old
	out.Name = FirstNonEmpty(fresh.Name, old.Name)
	out.Started = FirstNonEmpty(fresh.Started, old.Started)
	out.Ended = FirstNonEmpty(fresh.Ended, old.Ended)
	if fresh.CampaignID != 0 {
		out.CampaignID = fresh.CampaignID
	}

	index := make(map[string]int, len(old.Tracks))
	tracks := make([]NadeoTrack, len(old.Tracks))
	copy(tracks, old.Tracks)
	for i, t := range tracks {
		index[t.MapUID] = i
	}
	for _, ft := range fresh.Tracks {
		if i, ok := index[ft.MapUID]; ok {
			tracks[i] = mergeNadeoTrack(tracks[i], ft)
			continue
		}
		index[ft.MapUID] = len(tracks)
		tracks = append(tracks, ft)
	}
	sort.SliceStable(tracks, func(i, j int) bool { return tracks[i].Position < tracks[j].Position })
	out.Tracks = tracks
	return out
}

func mergeNadeoTrack(old, fresh NadeoTrack) NadeoTrack {
	out := old
	out.Position = fresh.Position
	out.MapID = FirstNonEmpty(fresh.MapID, old.MapID)
	out.Name = FirstNonEmpty(fresh.Name, old.Name)
	out.AuthorID = FirstNonEmpty(fresh.AuthorID, old.AuthorID)
	out.ThumbnailURL = FirstNonEmpty(fresh.ThumbnailURL, old.ThumbnailURL)
	out.FirstSeen = FirstNonEmpty(old.FirstSeen, fresh.FirstSeen)

	// A zero threshold means "this fetch didn't say", not "this map has no gold
	// medal" — so it never clobbers a value already captured.
	for _, f := range []struct{ dst, src *int }{
		{&out.AuthorMs, &fresh.AuthorMs},
		{&out.GoldMs, &fresh.GoldMs},
		{&out.SilverMs, &fresh.SilverMs},
		{&out.BronzeMs, &fresh.BronzeMs},
	} {
		if *f.src > 0 {
			*f.dst = *f.src
		}
	}

	// The inverted rule. A slower fresh time is not an improvement and not a
	// correction — it's the provider being inconsistent, or a leaderboard that
	// hasn't caught up — so the faster of the two wins, and a missing time
	// leaves the archived one alone.
	//
	// WorldRank/DrivenAt/RecordID/ReplayURL/NadeoMedal describe *the run*, not
	// the track, which makes them the one place the never-lose rule is the
	// wrong instinct:
	//
	//   - On a genuine improvement they are replaced wholesale, blanks
	//     included. The record now describes a different lap, and keeping the
	//     superseded run's date or replay against it would be a false
	//     statement rather than preserved history. The fallback path reports no
	//     date at all, so "blank" is the honest answer there.
	//   - On an identical time it's the same lap seen through a different
	//     endpoint, so missing fields are backfilled and nothing is
	//     overwritten. This is what lets the two paths complete each other: the
	//     Core records endpoint knows the date and replay, the Live leaderboard
	//     knows the rank, and neither knows the other's.
	switch {
	case fresh.PersonalBestMs > 0 && (out.PersonalBestMs == 0 || fresh.PersonalBestMs < out.PersonalBestMs):
		out.PersonalBestMs = fresh.PersonalBestMs
		out.WorldRank = fresh.WorldRank
		out.DrivenAt = fresh.DrivenAt
		out.RecordID = fresh.RecordID
		out.ReplayURL = fresh.ReplayURL
		out.NadeoMedal = fresh.NadeoMedal

	case fresh.PersonalBestMs > 0 && fresh.PersonalBestMs == out.PersonalBestMs:
		out.WorldRank = firstNonZero(fresh.WorldRank, out.WorldRank)
		out.DrivenAt = FirstNonEmpty(out.DrivenAt, fresh.DrivenAt)
		out.RecordID = FirstNonEmpty(out.RecordID, fresh.RecordID)
		out.ReplayURL = FirstNonEmpty(out.ReplayURL, fresh.ReplayURL)
		out.NadeoMedal = firstNonZero(out.NadeoMedal, fresh.NadeoMedal)
	}

	// The season best follows the same minimum-wins rule on its own axis. It
	// should never actually move once a campaign has closed — the leaderboard
	// is frozen — but it's merged rather than assigned so that a fetch which
	// couldn't reach the season-scoped endpoint can't blank it.
	if fresh.SeasonBestMs > 0 && (out.SeasonBestMs == 0 || fresh.SeasonBestMs < out.SeasonBestMs) {
		out.SeasonBestMs = fresh.SeasonBestMs
		out.SeasonDrivenAt = fresh.SeasonDrivenAt
	} else if fresh.SeasonBestMs > 0 && fresh.SeasonBestMs == out.SeasonBestMs {
		out.SeasonDrivenAt = FirstNonEmpty(out.SeasonDrivenAt, fresh.SeasonDrivenAt)
	}
	return out
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

// LoadNadeoRecord reads the archived Trackmania history for one account,
// returning nil (not an error) when nothing has been captured yet.
func LoadNadeoRecord(archiveDir, accountID string) (*NadeoRecord, error) {
	path := RecordPath(archiveDir, ProviderNadeo, accountID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rec NadeoRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &rec, nil
}

// SaveNadeoRecord folds a fresh fetch into whatever is already archived and
// writes the result, mirroring SaveRecord. The merge is what guarantees a
// refresh — or a single-season one — can only ever add.
func SaveNadeoRecord(archiveDir, accountID string, fresh *NadeoRecord) (string, error) {
	existing, err := LoadNadeoRecord(archiveDir, accountID)
	if err != nil {
		return "", err
	}

	rec := mergeNadeoRecord(existing, fresh)
	rec.Provider = ProviderNadeo
	if rec.ID == "" {
		rec.ID = accountID
	}
	if rec.Title == "" {
		rec.Title = "Trackmania"
	}
	rec.Updated = time.Now().In(SiteLocation).Format("2006-01-02")

	blob, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return "", err
	}
	path := RecordPath(archiveDir, ProviderNadeo, accountID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, append(blob, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// campaignSummaryFilename is the raw-free projection of a game's campaign
// history, written into its content bundle so Hugo can render it without
// reading archive/ — the same arrangement as achievement-summary.yaml, and
// covered by the same publishResources: false cascade.
const campaignSummaryFilename = "campaigns.yaml"

// CampaignSummary is what Hugo reads at build time.
//
// Seasons stay in the archive's own (chronological) order. Newest-first is a
// display choice, made by whatever reads this file — the same reasoning as
// AchievementSummary.Earned being stored oldest-first.
type CampaignSummary struct {
	AccountID   string `yaml:"account_id"`
	DisplayName string `yaml:"display_name,omitempty"`
	Fetched     string `yaml:"fetched,omitempty"`

	TracksDriven int         `yaml:"tracks_driven"`
	TracksTotal  int         `yaml:"tracks_total"`
	Medals       MedalCounts `yaml:"medals"`
	// Improved counts tracks whose medal got better after their campaign
	// closed — the going-back-to-clean-up total.
	Improved int `yaml:"improved,omitempty"`

	Seasons []SeasonSummary `yaml:"seasons,omitempty"`
}

// MedalCounts tallies medals by tier. None counts tracks driven that beat no
// threshold, which is why it can't just be inferred from the others.
type MedalCounts struct {
	Author int `yaml:"author"`
	Gold   int `yaml:"gold"`
	Silver int `yaml:"silver"`
	Bronze int `yaml:"bronze"`
	None   int `yaml:"none"`
}

func (m *MedalCounts) add(medal string) {
	switch medal {
	case "author":
		m.Author++
	case "gold":
		m.Gold++
	case "silver":
		m.Silver++
	case "bronze":
		m.Bronze++
	case "none":
		m.None++
	}
}

// SeasonSummary is one campaign's row in the projection.
type SeasonSummary struct {
	Name    string `yaml:"name"`
	UID     string `yaml:"uid"`
	Started string `yaml:"started,omitempty"`
	Ended   string `yaml:"ended,omitempty"`

	Driven int         `yaml:"driven"`
	Total  int         `yaml:"total"`
	Medals MedalCounts `yaml:"medals"`
	// Improved counts tracks whose medal got better after the campaign closed.
	Improved int `yaml:"improved,omitempty"`

	Tracks []TrackSummary `yaml:"tracks,omitempty"`
}

// TrackSummary is one cell of the campaign grid.
//
// Medal is derived from the times beside it, and stored anyway: the template
// then does no threshold arithmetic, while the thresholds themselves stay
// available for any other view. Data shaped for querying, not for one layout.
type TrackSummary struct {
	Position       int    `yaml:"position"`
	Number         string `yaml:"number"` // "01".."25", the label the game shows
	Name           string `yaml:"name,omitempty"`
	PersonalBestMs int    `yaml:"personal_best_ms,omitempty"`
	Medal          string `yaml:"medal,omitempty"`
	AuthorMs       int    `yaml:"author_ms,omitempty"`
	GoldMs         int    `yaml:"gold_ms,omitempty"`
	SilverMs       int    `yaml:"silver_ms,omitempty"`
	BronzeMs       int    `yaml:"bronze_ms,omitempty"`
	WorldRank      int    `yaml:"world_rank,omitempty"`
	// DrivenAt is when the run was set. Projected even though nothing renders
	// it yet: it's what would let campaign progress join the site timeline, and
	// re-projecting is free while re-fetching is not.
	DrivenAt string `yaml:"driven_at,omitempty"`

	// The season-scoped figures, and whether the medal improved after the
	// campaign closed. Improved is derived, and stored so the template doesn't
	// have to rank medals itself.
	SeasonBestMs   int    `yaml:"season_best_ms,omitempty"`
	SeasonMedal    string `yaml:"season_medal,omitempty"`
	SeasonDrivenAt string `yaml:"season_driven_at,omitempty"`
	Improved       bool   `yaml:"improved,omitempty"`
}

func campaignSummaryPath(gameDir string) string {
	return filepath.Join(gameDir, campaignSummaryFilename)
}

// WriteCampaignSummary recomputes the projection from whatever is currently
// archived for this account, not just from a record that was just fetched.
// A game with nothing archived gets no file; a stale one is removed. wrote
// reports whether a file now exists, matching WriteAchievementSummary.
func WriteCampaignSummary(archiveDir, gameDir, accountID string) (wrote bool, err error) {
	path := campaignSummaryPath(gameDir)

	var rec *NadeoRecord
	if accountID != "" {
		if rec, err = LoadNadeoRecord(archiveDir, accountID); err != nil {
			return false, err
		}
	}
	if rec == nil || len(rec.Campaigns) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}

	summary := CampaignSummary{
		AccountID:   rec.ID,
		DisplayName: rec.DisplayName,
		Fetched:     rec.Fetched,
	}
	for _, c := range rec.Campaigns {
		season := SeasonSummary{
			Name:    c.Name,
			UID:     c.SeasonUID,
			Started: c.Started,
			Ended:   c.Ended,
			Total:   len(c.Tracks),
		}
		for _, t := range c.Tracks {
			medal := t.Medal()
			if t.Driven() {
				season.Driven++
				season.Medals.add(medal)
				summary.Medals.add(medal)
			}
			if t.ImprovedSinceSeason() {
				season.Improved++
				summary.Improved++
			}
			season.Tracks = append(season.Tracks, TrackSummary{
				Position:       t.Position,
				Number:         fmt.Sprintf("%02d", t.Position+1),
				Name:           t.Name,
				PersonalBestMs: t.PersonalBestMs,
				Medal:          medal,
				AuthorMs:       t.AuthorMs,
				GoldMs:         t.GoldMs,
				SilverMs:       t.SilverMs,
				BronzeMs:       t.BronzeMs,
				WorldRank:      t.WorldRank,
				DrivenAt:       t.DrivenAt,
				SeasonBestMs:   t.SeasonBestMs,
				SeasonMedal:    t.SeasonMedal(),
				SeasonDrivenAt: t.SeasonDrivenAt,
				Improved:       t.ImprovedSinceSeason(),
			})
		}
		summary.TracksDriven += season.Driven
		summary.TracksTotal += season.Total
		summary.Seasons = append(summary.Seasons, season)
	}

	out, err := yaml.Marshal(summary)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// LoadCampaignSummary reads back what WriteCampaignSummary wrote, returning nil
// (not an error) when the game has no campaign history captured.
func LoadCampaignSummary(gameDir string) (*CampaignSummary, error) {
	path := campaignSummaryPath(gameDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s CampaignSummary
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}
