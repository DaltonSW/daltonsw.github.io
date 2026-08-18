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

// ProviderNinjaKiwi is BTD6, captured from Ninja Kiwi's own data API via a
// player's OAK save-export token.
//
// It is deliberately *not* in ProviderOrder, and deliberately not produced by
// BuildProviderLinks — same reasoning as ProviderNadeo. BTD6's Steam
// achievement pair already exists via steam_appid, in the normal
// achievement-summary.yaml path; this is a full save snapshot (tower mastery,
// map completion, hero unlocks, inventory...), not a second achievement set,
// and folding it into ProviderOrder would have nothing sane to sum it with.
//
// So it gets its own record type, its own merge, and its own projection,
// below — the same shape the Nadeo doc comment in nadeo.go already
// establishes for "a provider whose data isn't achievements".
const ProviderNinjaKiwi = "ninjakiwi"

// BTD6Record is one player's captured BTD6 save state. Unlike ArchiveRecord
// it is keyed by a Ninja Kiwi player id rather than a per-game id — the OAK
// token used to fetch it is a rotating credential, not a stable identifier,
// so it can't be the archive key.
type BTD6Record struct {
	Provider string `json:"provider"` // always ProviderNinjaKiwi
	Title    string `json:"title"`    // "Bloons TD 6"
	ID       string `json:"id"`       // the stable Ninja Kiwi player id

	Updated     string `json:"updated"`
	Fetched     string `json:"fetched,omitempty"`
	LastAttempt string `json:"last_attempt,omitempty"`
	LastError   string `json:"last_error,omitempty"`

	// Point-in-time stats: a fresh successful fetch always replaces these,
	// never max-merged. Both can legitimately go down — monkey money gets
	// spent, current trophies can be lost in PvP — so taking the larger of
	// two fetches would eventually just show the highest value ever seen
	// rather than the truth.
	MonkeyMoney int `json:"monkey_money,omitempty"`
	Trophies    int `json:"trophies,omitempty"`

	// Cumulative stats: these only grow over a save's life, so a fresh fetch
	// merges by max, the same rule mergeProvider applies to achievement
	// totals.
	XP                            int `json:"xp,omitempty"`
	Rank                          int `json:"rank,omitempty"`
	VeteranXP                     int `json:"veteran_xp,omitempty"`
	VeteranRank                   int `json:"veteran_rank,omitempty"`
	LifetimeTrophies              int `json:"lifetime_trophies,omitempty"`
	LifetimeTeamTrophies          int `json:"lifetime_team_trophies,omitempty"`
	KnowledgePoints               int `json:"knowledge_points,omitempty"`
	HighestSeenRound              int `json:"highest_seen_round,omitempty"`
	GamesPlayed                   int `json:"games_played,omitempty"`
	DailyRewardCount              int `json:"daily_reward_count,omitempty"`
	TotalDailyChallengesCompleted int `json:"total_daily_challenges_completed,omitempty"`
	TotalRacesEntered             int `json:"total_races_entered,omitempty"`
	ChallengesPlayed              int `json:"challenges_played,omitempty"`
	ChallengesShared              int `json:"challenges_shared,omitempty"`
	TotalCompletedOdysseys        int `json:"total_completed_odysseys,omitempty"`
	ContinuesUsed                 int `json:"continues_used,omitempty"`
	CollectionEventCratesOpened   int `json:"collection_event_crates_opened,omitempty"`

	// ConsecutiveDailyChallengesCompleted is a streak, not a lifetime total —
	// it resets when a streak breaks, so unlike the cumulative fields above it
	// is point-in-time and always takes the fresh value.
	ConsecutiveDailyChallengesCompleted int `json:"consecutive_daily_challenges_completed,omitempty"`

	// PrimaryHero is current state — a player can change it — so it's
	// point-in-time, taking the fresh value whenever the fetch reports one.
	PrimaryHero string `json:"primary_hero,omitempty"`

	UnlockedHeroes    map[string]bool `json:"unlocked_heroes,omitempty"`    // OR-merge
	TowerXP           map[string]int  `json:"tower_xp,omitempty"`           // max-merge per tower
	UnlockedTowers    map[string]bool `json:"unlocked_towers,omitempty"`    // OR-merge
	AcquiredUpgrades  map[string]bool `json:"acquired_upgrades,omitempty"`  // OR-merge
	AcquiredKnowledge map[string]bool `json:"acquired_knowledge,omitempty"` // OR-merge
	UnlockedSkins     map[string]bool `json:"unlocked_skins,omitempty"`     // OR-merge
	TrophyStoreItems  map[string]bool `json:"trophy_store_items,omitempty"` // OR-merge

	// InstaTowers is tower -> upgrade-tier code -> quantity in inventory.
	// Point-in-time per leaf: insta-monkeys are consumed in play, so a smaller
	// fresh count is a real spend, not missing data — the fresh value always
	// wins for a leaf it reports.
	InstaTowers map[string]map[string]int `json:"insta_towers,omitempty"`

	// Powers/PowersPro: Quantity is consumable inventory, same point-in-time
	// rule as InstaTowers. The Pro "seen"/tier fields are progress markers
	// that only move forward, so those merge like the cumulative stats above.
	Powers    map[string]BTD6Power    `json:"powers,omitempty"`
	PowersPro map[string]BTD6PowerPro `json:"powers_pro,omitempty"`

	// AchievementsClaimed is Ninja Kiwi's own in-game achievement list — a
	// different namespace from Steam's (kept for completeness, not folded
	// into achievement-summary.yaml). Union by name.
	AchievementsClaimed []string `json:"achievements_claimed,omitempty"`

	// MapProgress is keyed by map name, then difficulty ("Easy"/"Medium"/
	// "Hard"), then single/co-op, then game mode. Completion flags OR-merge;
	// BestRound/TimesCompleted max-merge, matching the cumulative rule.
	MapProgress map[string]BTD6MapEntry `json:"map_progress,omitempty"`

	// Raw holds the verbatim save body behind the most recent successful
	// fetch, replaced wholesale each time — the same rule mergeProvider
	// applies to ArchiveRecord.Raw, and NadeoRecord.Raw before it. This
	// payload has far more fields than are modeled above; Raw is what makes
	// the rest recoverable without a re-fetch, which an expired OAK token may
	// make impossible until the next export.
	Raw *json.RawMessage `json:"raw,omitempty"`
}

type BTD6Power struct {
	Quantity int  `json:"quantity"`
	IsNew    bool `json:"is_new,omitempty"`
}

type BTD6PowerPro struct {
	IsSeen         bool `json:"is_seen,omitempty"`
	XP             int  `json:"xp,omitempty"`
	UnlockedTier   int  `json:"unlocked_tier,omitempty"`
	SeenUnlockPro  bool `json:"seen_unlock_pro,omitempty"`
	SeenUnlockTier bool `json:"seen_unlock_tier,omitempty"`
}

type BTD6MapEntry struct {
	Complete   bool                           `json:"complete,omitempty"`
	Difficulty map[string]BTD6DifficultyEntry `json:"difficulty,omitempty"`
}

type BTD6DifficultyEntry struct {
	Single map[string]BTD6ModeResult `json:"single,omitempty"`
	Coop   map[string]BTD6ModeResult `json:"coop,omitempty"`
}

type BTD6ModeResult struct {
	Completed                   bool `json:"completed,omitempty"`
	CompletedWithoutLoadingSave bool `json:"completed_without_loading_save,omitempty"`
	BestRound                   int  `json:"best_round,omitempty"`
	TimesCompleted              int  `json:"times_completed,omitempty"`
}

// hasData reports whether a fetch actually produced a save snapshot.
func (r *BTD6Record) hasData() bool {
	return r != nil && r.Fetched != ""
}

// mergeNinjaKiwiRecord folds a fresh fetch into what's already archived. It
// carries the same promise as mergeProvider — **a refresh must never be able
// to lose data** — but a save snapshot isn't a growing list of unlocks the
// way achievements or campaign tracks are, so most of the work is deciding,
// field by field, whether "bigger/present" or "freshest" is the safe answer:
//
//   - Lifetime counters (XP, rank, highest round, games played, etc.) only
//     grow, so they merge by max — the same rule mergeProvider applies to
//     achievement totals.
//   - Spendable/consumable state (monkey money, current trophies, insta-monkey
//     and power quantities) can legitimately go *down* between fetches. Taking
//     the max there would make the archive drift upward from reality forever;
//     the fresh value always wins instead.
//   - Unlock maps (towers, heroes, upgrades, knowledge, skins, trophy-store
//     items) only ever gain entries in play, so they merge by OR: true, once
//     seen, never reverts to false or absent.
//
// Get this wrong in either direction and it's not just "reads oddly" the way
// achievement counts are unaffected by real player state — it either lies
// about currency the player already spent, or forgets an unlock they earned.
func mergeNinjaKiwiRecord(old, fresh *BTD6Record) *BTD6Record {
	if old == nil {
		return fresh
	}
	if fresh == nil {
		return old
	}

	merged := *old
	merged.LastAttempt = fresh.LastAttempt
	merged.LastError = fresh.LastError

	// A fetch that produced nothing usable updates only the attempt
	// metadata. A revoked/expired OAK token and a transient API error both
	// land here, and neither should erase what's already captured.
	if fresh.LastError != "" || !fresh.hasData() {
		return &merged
	}

	merged.Fetched = fresh.Fetched
	merged.Title = FirstNonEmpty(fresh.Title, merged.Title)
	if fresh.ID != "" {
		merged.ID = fresh.ID
	}
	if fresh.Raw != nil {
		merged.Raw = fresh.Raw
	}

	// Point-in-time: always the fresh value.
	merged.MonkeyMoney = fresh.MonkeyMoney
	merged.Trophies = fresh.Trophies
	merged.ConsecutiveDailyChallengesCompleted = fresh.ConsecutiveDailyChallengesCompleted
	merged.PrimaryHero = FirstNonEmpty(fresh.PrimaryHero, merged.PrimaryHero)

	// Cumulative: max-merge.
	merged.XP = maxInt(merged.XP, fresh.XP)
	merged.Rank = maxInt(merged.Rank, fresh.Rank)
	merged.VeteranXP = maxInt(merged.VeteranXP, fresh.VeteranXP)
	merged.VeteranRank = maxInt(merged.VeteranRank, fresh.VeteranRank)
	merged.LifetimeTrophies = maxInt(merged.LifetimeTrophies, fresh.LifetimeTrophies)
	merged.LifetimeTeamTrophies = maxInt(merged.LifetimeTeamTrophies, fresh.LifetimeTeamTrophies)
	merged.KnowledgePoints = maxInt(merged.KnowledgePoints, fresh.KnowledgePoints)
	merged.HighestSeenRound = maxInt(merged.HighestSeenRound, fresh.HighestSeenRound)
	merged.GamesPlayed = maxInt(merged.GamesPlayed, fresh.GamesPlayed)
	merged.DailyRewardCount = maxInt(merged.DailyRewardCount, fresh.DailyRewardCount)
	merged.TotalDailyChallengesCompleted = maxInt(merged.TotalDailyChallengesCompleted, fresh.TotalDailyChallengesCompleted)
	merged.TotalRacesEntered = maxInt(merged.TotalRacesEntered, fresh.TotalRacesEntered)
	merged.ChallengesPlayed = maxInt(merged.ChallengesPlayed, fresh.ChallengesPlayed)
	merged.ChallengesShared = maxInt(merged.ChallengesShared, fresh.ChallengesShared)
	merged.TotalCompletedOdysseys = maxInt(merged.TotalCompletedOdysseys, fresh.TotalCompletedOdysseys)
	merged.ContinuesUsed = maxInt(merged.ContinuesUsed, fresh.ContinuesUsed)
	merged.CollectionEventCratesOpened = maxInt(merged.CollectionEventCratesOpened, fresh.CollectionEventCratesOpened)

	merged.UnlockedHeroes = mergeBoolMap(merged.UnlockedHeroes, fresh.UnlockedHeroes)
	merged.UnlockedTowers = mergeBoolMap(merged.UnlockedTowers, fresh.UnlockedTowers)
	merged.AcquiredUpgrades = mergeBoolMap(merged.AcquiredUpgrades, fresh.AcquiredUpgrades)
	merged.AcquiredKnowledge = mergeBoolMap(merged.AcquiredKnowledge, fresh.AcquiredKnowledge)
	merged.UnlockedSkins = mergeBoolMap(merged.UnlockedSkins, fresh.UnlockedSkins)
	merged.TrophyStoreItems = mergeBoolMap(merged.TrophyStoreItems, fresh.TrophyStoreItems)
	merged.TowerXP = mergeMaxIntMap(merged.TowerXP, fresh.TowerXP)

	merged.InstaTowers = mergeInstaTowers(merged.InstaTowers, fresh.InstaTowers)
	merged.Powers = mergePowers(merged.Powers, fresh.Powers)
	merged.PowersPro = mergePowersPro(merged.PowersPro, fresh.PowersPro)
	merged.AchievementsClaimed = mergeStringSet(merged.AchievementsClaimed, fresh.AchievementsClaimed)
	merged.MapProgress = mergeMapProgress(merged.MapProgress, fresh.MapProgress)

	return &merged
}

func maxInt(a, b int) int {
	if b > a {
		return b
	}
	return a
}

// mergeBoolMap ORs two unlock maps: once true, always true.
func mergeBoolMap(old, fresh map[string]bool) map[string]bool {
	if len(fresh) == 0 {
		return old
	}
	out := make(map[string]bool, len(old)+len(fresh))
	for k, v := range old {
		out[k] = v
	}
	for k, v := range fresh {
		out[k] = out[k] || v
	}
	return out
}

func mergeMaxIntMap(old, fresh map[string]int) map[string]int {
	if len(fresh) == 0 {
		return old
	}
	out := make(map[string]int, len(old)+len(fresh))
	for k, v := range old {
		out[k] = v
	}
	for k, v := range fresh {
		out[k] = maxInt(out[k], v)
	}
	return out
}

// mergeInstaTowers replaces each leaf the fresh fetch reports (insta-monkeys
// are spent in play, so the fresh count is authoritative for any tower/tier it
// names) while leaving towers or tiers the fetch didn't mention untouched.
func mergeInstaTowers(old, fresh map[string]map[string]int) map[string]map[string]int {
	if len(fresh) == 0 {
		return old
	}
	out := make(map[string]map[string]int, len(old))
	for tower, tiers := range old {
		cp := make(map[string]int, len(tiers))
		for tier, qty := range tiers {
			cp[tier] = qty
		}
		out[tower] = cp
	}
	for tower, tiers := range fresh {
		if out[tower] == nil {
			out[tower] = make(map[string]int, len(tiers))
		}
		for tier, qty := range tiers {
			out[tower][tier] = qty
		}
	}
	return out
}

// mergePowers replaces each power the fresh fetch reports wholesale —
// quantity is consumable, so a present entry is always authoritative.
func mergePowers(old, fresh map[string]BTD6Power) map[string]BTD6Power {
	if len(fresh) == 0 {
		return old
	}
	out := make(map[string]BTD6Power, len(old)+len(fresh))
	for k, v := range old {
		out[k] = v
	}
	for k, v := range fresh {
		out[k] = v
	}
	return out
}

// mergePowersPro merges each pro power's progress markers forward: the "seen"
// flags OR, the tier/xp fields max — the same cumulative rule as the
// top-level stats, because unlocking a higher pro tier is permanent progress.
func mergePowersPro(old, fresh map[string]BTD6PowerPro) map[string]BTD6PowerPro {
	if len(fresh) == 0 {
		return old
	}
	out := make(map[string]BTD6PowerPro, len(old)+len(fresh))
	for k, v := range old {
		out[k] = v
	}
	for k, f := range fresh {
		o := out[k]
		out[k] = BTD6PowerPro{
			IsSeen:         o.IsSeen || f.IsSeen,
			XP:             maxInt(o.XP, f.XP),
			UnlockedTier:   maxInt(o.UnlockedTier, f.UnlockedTier),
			SeenUnlockPro:  o.SeenUnlockPro || f.SeenUnlockPro,
			SeenUnlockTier: o.SeenUnlockTier || f.SeenUnlockTier,
		}
	}
	return out
}

func mergeStringSet(old, fresh []string) []string {
	if len(fresh) == 0 {
		return old
	}
	seen := make(map[string]bool, len(old))
	out := make([]string, len(old))
	copy(out, old)
	for _, s := range out {
		seen[s] = true
	}
	for _, s := range fresh {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func mergeMapProgress(old, fresh map[string]BTD6MapEntry) map[string]BTD6MapEntry {
	if len(fresh) == 0 {
		return old
	}
	out := make(map[string]BTD6MapEntry, len(old)+len(fresh))
	for k, v := range old {
		out[k] = v
	}
	for name, f := range fresh {
		out[name] = mergeMapEntry(out[name], f)
	}
	return out
}

func mergeMapEntry(old, fresh BTD6MapEntry) BTD6MapEntry {
	out := BTD6MapEntry{Complete: old.Complete || fresh.Complete}
	if len(old.Difficulty) == 0 && len(fresh.Difficulty) == 0 {
		return out
	}
	out.Difficulty = make(map[string]BTD6DifficultyEntry, len(old.Difficulty)+len(fresh.Difficulty))
	for k, v := range old.Difficulty {
		out.Difficulty[k] = v
	}
	for diff, f := range fresh.Difficulty {
		out.Difficulty[diff] = mergeDifficultyEntry(out.Difficulty[diff], f)
	}
	return out
}

func mergeDifficultyEntry(old, fresh BTD6DifficultyEntry) BTD6DifficultyEntry {
	return BTD6DifficultyEntry{
		Single: mergeModeResults(old.Single, fresh.Single),
		Coop:   mergeModeResults(old.Coop, fresh.Coop),
	}
}

func mergeModeResults(old, fresh map[string]BTD6ModeResult) map[string]BTD6ModeResult {
	if len(old) == 0 && len(fresh) == 0 {
		return nil
	}
	out := make(map[string]BTD6ModeResult, len(old)+len(fresh))
	for k, v := range old {
		out[k] = v
	}
	for mode, f := range fresh {
		o := out[mode]
		out[mode] = BTD6ModeResult{
			Completed:                   o.Completed || f.Completed,
			CompletedWithoutLoadingSave: o.CompletedWithoutLoadingSave || f.CompletedWithoutLoadingSave,
			BestRound:                   maxInt(o.BestRound, f.BestRound),
			TimesCompleted:              maxInt(o.TimesCompleted, f.TimesCompleted),
		}
	}
	return out
}

// LoadNinjaKiwiRecord reads the archived BTD6 save for one player, returning
// nil (not an error) when nothing has been captured yet.
func LoadNinjaKiwiRecord(archiveDir, userID string) (*BTD6Record, error) {
	path := RecordPath(archiveDir, ProviderNinjaKiwi, userID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rec BTD6Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &rec, nil
}

// SaveNinjaKiwiRecord folds a fresh fetch into whatever is already archived
// and writes the result, mirroring SaveNadeoRecord/SaveRecord.
func SaveNinjaKiwiRecord(archiveDir, userID string, fresh *BTD6Record) (string, error) {
	existing, err := LoadNinjaKiwiRecord(archiveDir, userID)
	if err != nil {
		return "", err
	}

	rec := mergeNinjaKiwiRecord(existing, fresh)
	rec.Provider = ProviderNinjaKiwi
	if rec.ID == "" {
		rec.ID = userID
	}
	if rec.Title == "" {
		rec.Title = "Bloons TD 6"
	}
	rec.Updated = time.Now().In(SiteLocation).Format("2006-01-02")

	blob, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return "", err
	}
	path := RecordPath(archiveDir, ProviderNinjaKiwi, userID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, append(blob, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// btd6SummaryFilename is the raw-free projection of a player's BTD6 save,
// written into the game's content bundle so Hugo can render it without
// reading archive/ — the same arrangement as achievement-summary.yaml and
// campaigns.yaml, and covered by the same publishResources: false cascade.
const btd6SummaryFilename = "btd6-summary.yaml"

// BTD6Summary is the curated slice of a save Hugo reads at build time. It
// deliberately doesn't carry everything BTD6Record captures — skins, the
// trophy store, and most of the powers-pro detail are archived but not
// projected yet, which is fine: re-projecting from the archive is free, and
// display can grow into more of it without another fetch.
type BTD6Summary struct {
	UserID  string `yaml:"user_id"`
	Fetched string `yaml:"fetched,omitempty"`

	XP                int `yaml:"xp"`
	Rank              int `yaml:"rank"`
	Trophies          int `yaml:"trophies"`
	LifetimeTrophies  int `yaml:"lifetime_trophies"`
	KnowledgePoints   int `yaml:"knowledge_points"`
	HighestSeenRound  int `yaml:"highest_seen_round"`
	GamesPlayed       int `yaml:"games_played"`
	TotalOdysseys     int `yaml:"total_odysseys"`
	AchievementsCount int `yaml:"achievements_count"`

	PrimaryHero string `yaml:"primary_hero,omitempty"`

	Towers []BTD6TowerSummary `yaml:"towers,omitempty"`
	Heroes []BTD6HeroSummary  `yaml:"heroes,omitempty"`
	Maps   []BTD6MapSummary   `yaml:"maps,omitempty"`

	InstaMonkeys      []BTD6InstaTowerSummary `yaml:"insta_monkeys,omitempty"`
	InstaMonkeysTotal int                     `yaml:"insta_monkeys_total,omitempty"`
}

type BTD6TowerSummary struct {
	Name     string `yaml:"name"`
	XP       int    `yaml:"xp"`
	Unlocked bool   `yaml:"unlocked"`
}

type BTD6HeroSummary struct {
	Name     string `yaml:"name"`
	Unlocked bool   `yaml:"unlocked"`
	Primary  bool   `yaml:"primary,omitempty"`
}

// BTD6MapSummary is one map's row in the completion grid. Modes is flattened
// out of the archive's difficulty/single-or-coop nesting into a sparse list —
// most maps don't offer every mode, so a dense difficulty x mode x coop grid
// would mostly be blank cells.
type BTD6MapSummary struct {
	Name     string               `yaml:"name"`
	Complete bool                 `yaml:"complete"`
	Modes    []BTD6MapModeSummary `yaml:"modes,omitempty"`
}

type BTD6MapModeSummary struct {
	Difficulty                  string `yaml:"difficulty"`
	Mode                        string `yaml:"mode"`
	Coop                        bool   `yaml:"coop,omitempty"`
	Completed                   bool   `yaml:"completed"`
	CompletedWithoutLoadingSave bool   `yaml:"completed_without_loading_save,omitempty"`
	BestRound                   int    `yaml:"best_round,omitempty"`
	TimesCompleted              int    `yaml:"times_completed,omitempty"`
}

// BTD6InstaTowerSummary is one tower's insta-monkey inventory, tier codes
// sorted for a stable grid column order.
type BTD6InstaTowerSummary struct {
	Tower string                 `yaml:"tower"`
	Total int                    `yaml:"total"`
	Tiers []BTD6InstaTierSummary `yaml:"tiers,omitempty"`
}

type BTD6InstaTierSummary struct {
	Tier     string `yaml:"tier"`
	Quantity int    `yaml:"quantity"`
}

func btd6SummaryPath(gameDir string) string {
	return filepath.Join(gameDir, btd6SummaryFilename)
}

// WriteBTD6Summary recomputes the projection from whatever is currently
// archived for this player, not just a record that was just fetched. A game
// with nothing archived gets no file; a stale one is removed. wrote reports
// whether a file now exists, matching WriteCampaignSummary/
// WriteAchievementSummary.
func WriteBTD6Summary(archiveDir, gameDir, userID string) (wrote bool, err error) {
	path := btd6SummaryPath(gameDir)

	var rec *BTD6Record
	if userID != "" {
		if rec, err = LoadNinjaKiwiRecord(archiveDir, userID); err != nil {
			return false, err
		}
	}
	if !rec.hasData() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}

	summary := BTD6Summary{
		UserID:            rec.ID,
		Fetched:           rec.Fetched,
		XP:                rec.XP,
		Rank:              rec.Rank,
		Trophies:          rec.Trophies,
		LifetimeTrophies:  rec.LifetimeTrophies,
		KnowledgePoints:   rec.KnowledgePoints,
		HighestSeenRound:  rec.HighestSeenRound,
		GamesPlayed:       rec.GamesPlayed,
		TotalOdysseys:     rec.TotalCompletedOdysseys,
		AchievementsCount: len(rec.AchievementsClaimed),
		PrimaryHero:       rec.PrimaryHero,
	}

	for name, unlocked := range rec.UnlockedTowers {
		summary.Towers = append(summary.Towers, BTD6TowerSummary{
			Name: name, XP: rec.TowerXP[name], Unlocked: unlocked,
		})
	}
	// A tower with recorded XP but missing from unlockedTowers (shouldn't
	// happen, but the archive doesn't assume it can't) still gets a row.
	for name, xp := range rec.TowerXP {
		if _, ok := rec.UnlockedTowers[name]; !ok {
			summary.Towers = append(summary.Towers, BTD6TowerSummary{Name: name, XP: xp})
		}
	}
	sort.Slice(summary.Towers, func(i, j int) bool { return summary.Towers[i].Name < summary.Towers[j].Name })

	for name, unlocked := range rec.UnlockedHeroes {
		summary.Heroes = append(summary.Heroes, BTD6HeroSummary{
			Name: name, Unlocked: unlocked, Primary: name == rec.PrimaryHero,
		})
	}
	sort.Slice(summary.Heroes, func(i, j int) bool { return summary.Heroes[i].Name < summary.Heroes[j].Name })

	for name, entry := range rec.MapProgress {
		m := BTD6MapSummary{Name: name, Complete: entry.Complete}
		for diff, de := range entry.Difficulty {
			for mode, r := range de.Single {
				m.Modes = append(m.Modes, BTD6MapModeSummary{
					Difficulty: diff, Mode: mode, Completed: r.Completed,
					CompletedWithoutLoadingSave: r.CompletedWithoutLoadingSave,
					BestRound:                   r.BestRound, TimesCompleted: r.TimesCompleted,
				})
			}
			for mode, r := range de.Coop {
				m.Modes = append(m.Modes, BTD6MapModeSummary{
					Difficulty: diff, Mode: mode, Coop: true, Completed: r.Completed,
					CompletedWithoutLoadingSave: r.CompletedWithoutLoadingSave,
					BestRound:                   r.BestRound, TimesCompleted: r.TimesCompleted,
				})
			}
		}
		sort.Slice(m.Modes, func(i, j int) bool {
			a, b := m.Modes[i], m.Modes[j]
			if a.Difficulty != b.Difficulty {
				return a.Difficulty < b.Difficulty
			}
			if a.Coop != b.Coop {
				return !a.Coop
			}
			return a.Mode < b.Mode
		})
		summary.Maps = append(summary.Maps, m)
	}
	sort.Slice(summary.Maps, func(i, j int) bool { return summary.Maps[i].Name < summary.Maps[j].Name })

	for tower, tiers := range rec.InstaTowers {
		row := BTD6InstaTowerSummary{Tower: tower}
		for tier, qty := range tiers {
			if qty <= 0 {
				continue
			}
			row.Tiers = append(row.Tiers, BTD6InstaTierSummary{Tier: tier, Quantity: qty})
			row.Total += qty
		}
		if row.Total == 0 {
			continue
		}
		sort.Slice(row.Tiers, func(i, j int) bool { return row.Tiers[i].Tier < row.Tiers[j].Tier })
		summary.InstaMonkeys = append(summary.InstaMonkeys, row)
		summary.InstaMonkeysTotal += row.Total
	}
	sort.Slice(summary.InstaMonkeys, func(i, j int) bool { return summary.InstaMonkeys[i].Tower < summary.InstaMonkeys[j].Tower })

	out, err := yaml.Marshal(summary)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// LoadBTD6Summary reads back what WriteBTD6Summary wrote, returning nil (not
// an error) when the game has no BTD6 save data captured.
func LoadBTD6Summary(gameDir string) (*BTD6Summary, error) {
	path := btd6SummaryPath(gameDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s BTD6Summary
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}
