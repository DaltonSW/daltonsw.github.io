package ninjakiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// today formats the current instant as the calendar date it falls on in the
// site's timezone. Duplicated from every other provider package on purpose —
// see the identical comment in providers/nadeo/fetch.go.
func today() string { return time.Now().In(model.SiteLocation).Format("2006-01-02") }

// rawSave is the subset of Ninja Kiwi's BTD6 save body this tool models.
// Confirmed against a live response, not documentation — Ninja Kiwi publishes
// none for this endpoint. Anything not listed here is still preserved,
// verbatim, in BTD6Record.Raw.
type rawSave struct {
	MonkeyMoney int `json:"monkeyMoney"`
	Trophies    int `json:"trophies"`

	XP                   int `json:"xp"`
	Rank                 int `json:"rank"`
	VeteranXP            int `json:"veteranXp"`
	VeteranRank          int `json:"veteranRank"`
	LifetimeTrophies     int `json:"lifetimeTrophies"`
	LifetimeTeamTrophies int `json:"lifetimeTeamTrophies"`
	KnowledgePoints      int `json:"knowledgePoints"`
	HighestSeenRound     int `json:"highestSeenRound"`
	GamesPlayed          int `json:"gamesPlayed"`

	DailyRewardCount                    int `json:"dailyRewardCount"`
	TotalDailyChallengesCompleted       int `json:"totalDailyChallengesCompleted"`
	ConsecutiveDailyChallengesCompleted int `json:"consecutiveDailyChallengesCompleted"`
	TotalRacesEntered                   int `json:"totalRacesEntered"`
	ChallengesPlayed                    int `json:"challengesPlayed"`
	ChallengesShared                    int `json:"challengesShared"`
	TotalCompletedOdysseys              int `json:"totalCompletedOdysseys"`
	ContinuesUsed                       int `json:"continuesUsed"`
	CollectionEventCratesOpened         int `json:"collectionEventCratesOpened"`

	PrimaryHero       string          `json:"primaryHero"`
	UnlockedHeroes    map[string]bool `json:"unlockedHeroes"`
	TowerXP           map[string]int  `json:"towerXP"`
	UnlockedTowers    map[string]bool `json:"unlockedTowers"`
	AcquiredUpgrades  map[string]bool `json:"acquiredUpgrades"`
	AcquiredKnowledge map[string]bool `json:"acquiredKnowledge"`
	UnlockedSkins     map[string]bool `json:"unlockedSkins"`
	TrophyStoreItems  map[string]bool `json:"trophyStoreItems"`

	InstaTowers map[string]map[string]int `json:"instaTowers"`

	Powers    map[string]rawPower    `json:"powers"`
	PowersPro map[string]rawPowerPro `json:"powersPro"`

	AchievementsClaimed []string `json:"achievementsClaimed"`

	MapProgress map[string]rawMapEntry `json:"mapProgress"`
}

type rawPower struct {
	Quantity int  `json:"quantity"`
	IsNew    bool `json:"isNew"`
}

// rawPowerPro's "seen" flags are 0/1 integers on the wire, not JSON booleans
// — confirmed against a live response, which is why these are typed int and
// converted below rather than decoded straight into model.BTD6PowerPro.
type rawPowerPro struct {
	IsSeen         int `json:"isSeen"`
	XP             int `json:"xp"`
	UnlockedTier   int `json:"unlockedTier"`
	SeenUnlockPro  int `json:"seenUnlockPro"`
	SeenUnlockTier int `json:"seenUnlockTier"`
}

type rawMapEntry struct {
	Complete   bool                         `json:"complete"`
	Difficulty map[string]rawDifficultyMode `json:"difficulty"`
}

type rawDifficultyMode struct {
	Single map[string]rawModeResult `json:"single"`
	Coop   map[string]rawModeResult `json:"coop"`
}

type rawModeResult struct {
	Completed                   bool `json:"completed"`
	CompletedWithoutLoadingSave bool `json:"completedWithoutLoadingSave"`
	BestRound                   int  `json:"bestRound"`
	TimesCompleted              int  `json:"timesCompleted"`
}

// FetchRecord captures one player's BTD6 save.
//
// Following this tool's contract for every provider: a fetch that fails is
// not a Go error. It records LastError on the returned record and returns
// (rec, nil), leaving SaveNinjaKiwiRecord to keep whatever is already
// archived. A non-nil error is reserved for an unconfigured client, which
// can't produce anything this run.
func FetchRecord(ctx context.Context, c *Client, userID string) (*model.BTD6Record, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("ninjakiwi: no OAK token configured")
	}

	rec := &model.BTD6Record{
		Provider:    model.ProviderNinjaKiwi,
		Title:       "Bloons TD 6",
		ID:          userID,
		LastAttempt: today(),
	}

	body, err := c.SaveBody(ctx)
	if err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}

	var raw rawSave
	if err := json.Unmarshal(body, &raw); err != nil {
		rec.LastError = err.Error()
		return rec, nil
	}

	rec.MonkeyMoney = raw.MonkeyMoney
	rec.Trophies = raw.Trophies
	rec.XP = raw.XP
	rec.Rank = raw.Rank
	rec.VeteranXP = raw.VeteranXP
	rec.VeteranRank = raw.VeteranRank
	rec.LifetimeTrophies = raw.LifetimeTrophies
	rec.LifetimeTeamTrophies = raw.LifetimeTeamTrophies
	rec.KnowledgePoints = raw.KnowledgePoints
	rec.HighestSeenRound = raw.HighestSeenRound
	rec.GamesPlayed = raw.GamesPlayed
	rec.DailyRewardCount = raw.DailyRewardCount
	rec.TotalDailyChallengesCompleted = raw.TotalDailyChallengesCompleted
	rec.ConsecutiveDailyChallengesCompleted = raw.ConsecutiveDailyChallengesCompleted
	rec.TotalRacesEntered = raw.TotalRacesEntered
	rec.ChallengesPlayed = raw.ChallengesPlayed
	rec.ChallengesShared = raw.ChallengesShared
	rec.TotalCompletedOdysseys = raw.TotalCompletedOdysseys
	rec.ContinuesUsed = raw.ContinuesUsed
	rec.CollectionEventCratesOpened = raw.CollectionEventCratesOpened
	rec.PrimaryHero = raw.PrimaryHero
	rec.UnlockedHeroes = raw.UnlockedHeroes
	rec.TowerXP = raw.TowerXP
	rec.UnlockedTowers = raw.UnlockedTowers
	rec.AcquiredUpgrades = raw.AcquiredUpgrades
	rec.AcquiredKnowledge = raw.AcquiredKnowledge
	rec.UnlockedSkins = raw.UnlockedSkins
	rec.TrophyStoreItems = raw.TrophyStoreItems
	rec.InstaTowers = raw.InstaTowers
	rec.AchievementsClaimed = raw.AchievementsClaimed

	if len(raw.Powers) > 0 {
		rec.Powers = make(map[string]model.BTD6Power, len(raw.Powers))
		for k, p := range raw.Powers {
			rec.Powers[k] = model.BTD6Power{Quantity: p.Quantity, IsNew: p.IsNew}
		}
	}
	if len(raw.PowersPro) > 0 {
		rec.PowersPro = make(map[string]model.BTD6PowerPro, len(raw.PowersPro))
		for k, p := range raw.PowersPro {
			rec.PowersPro[k] = model.BTD6PowerPro{
				IsSeen: p.IsSeen != 0, XP: p.XP, UnlockedTier: p.UnlockedTier,
				SeenUnlockPro: p.SeenUnlockPro != 0, SeenUnlockTier: p.SeenUnlockTier != 0,
			}
		}
	}
	if len(raw.MapProgress) > 0 {
		rec.MapProgress = make(map[string]model.BTD6MapEntry, len(raw.MapProgress))
		for mapName, m := range raw.MapProgress {
			entry := model.BTD6MapEntry{Complete: m.Complete}
			if len(m.Difficulty) > 0 {
				entry.Difficulty = make(map[string]model.BTD6DifficultyEntry, len(m.Difficulty))
				for diff, dm := range m.Difficulty {
					entry.Difficulty[diff] = model.BTD6DifficultyEntry{
						Single: convertModeResults(dm.Single),
						Coop:   convertModeResults(dm.Coop),
					}
				}
			}
			rec.MapProgress[mapName] = entry
		}
	}

	rec.Raw = &body
	rec.Fetched = today()
	return rec, nil
}

func convertModeResults(in map[string]rawModeResult) map[string]model.BTD6ModeResult {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]model.BTD6ModeResult, len(in))
	for mode, r := range in {
		out[mode] = model.BTD6ModeResult{
			Completed:                   r.Completed,
			CompletedWithoutLoadingSave: r.CompletedWithoutLoadingSave,
			BestRound:                   r.BestRound,
			TimesCompleted:              r.TimesCompleted,
		}
	}
	return out
}
