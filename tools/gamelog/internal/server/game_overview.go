package server

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/model"
)

// gameOverview is the read-only numbers panel on a game's page: completion per
// provider, playtime, and the per-game stats a provider reported. It reads the
// same projections the site does, so it can't disagree with the published page.
type gameOverview struct {
	Unlocked, Total int
	Percent         string // "46.0%", blank when there is no denominator
	Playtime        string
	LastPlayed      string
	Mastered        string
	Providers       []overviewProvider // only when more than one: one provider's numbers are the totals above
	Rarest          []model.EarnedAchievement
	Stats           []overviewStats
}

type overviewProvider struct {
	Label, Percent, Playtime, LastPlayed string
	Unlocked, Total                      int
}

type overviewStats struct {
	Label   string
	Fetched string
	Rows    []statRow
}

type statRow struct{ Key, Label, Value string }

// rarestShown is how many of the rarest earned achievements the panel lists.
const rarestShown = 5

func pct(n, total int) string {
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(total))
}

// formatStatValue mirrors layouts/partials/game-stats.html: grouped integers,
// two decimals for fractions, three significant figures for tiny values.
func formatStatValue(v float64) string {
	switch {
	case v != 0 && math.Abs(v) <= 0.01:
		return strconv.FormatFloat(v, 'g', 3, 64)
	case v != math.Floor(v):
		return fmt.Sprintf("%.2f", v)
	}
	s := strconv.FormatInt(int64(math.Abs(v)), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if v < 0 {
		s = "-" + s
	}
	return s
}

// buildGameOverview returns nil when the game has neither achievements nor stats.
func buildGameOverview(gameDir string) *gameOverview {
	sum, _ := model.LoadAchievementSummary(gameDir) // a broken projection hides the panel, not the page
	stats, _ := model.LoadStatsSummary(gameDir)
	if (sum == nil || sum.Total == 0) && (stats == nil || len(stats.Providers) == 0) {
		return nil
	}
	o := &gameOverview{}
	if sum != nil {
		o.Unlocked, o.Total = sum.Unlocked, sum.Total
		o.Percent = pct(sum.Unlocked, sum.Total)
		if sum.PlaytimeMins > 0 {
			o.Playtime = commands.FormatHours(sum.PlaytimeMins)
		}
		o.LastPlayed, o.Mastered = sum.LastPlayed, sum.Mastered
		if len(sum.Providers) > 1 {
			for _, p := range sum.Providers {
				op := overviewProvider{
					Label:      p.Platform,
					Unlocked:   p.Unlocked,
					Total:      p.Total,
					Percent:    pct(p.Unlocked, p.Total),
					LastPlayed: p.LastPlayed,
				}
				if op.Label == "" {
					op.Label = model.ProviderLabel(p.Provider)
				}
				if p.PlaytimeMins > 0 {
					op.Playtime = commands.FormatHours(p.PlaytimeMins)
				}
				o.Providers = append(o.Providers, op)
			}
		}
		for _, a := range sum.Earned {
			if a.Rarity > 0 {
				o.Rarest = append(o.Rarest, a)
			}
		}
		sort.SliceStable(o.Rarest, func(i, j int) bool { return o.Rarest[i].Rarity < o.Rarest[j].Rarity })
		if len(o.Rarest) > rarestShown {
			o.Rarest = o.Rarest[:rarestShown]
		}
	}
	if stats != nil {
		multi := len(stats.Providers) > 1
		for _, p := range stats.Providers {
			st := overviewStats{Fetched: p.Fetched}
			if multi {
				st.Label = p.Platform
				if st.Label == "" {
					st.Label = model.ProviderLabel(p.Provider)
				}
			}
			for _, s := range p.Stats {
				label := s.Name
				if label == "" {
					label = strings.ReplaceAll(s.Key, "_", " ")
				}
				st.Rows = append(st.Rows, statRow{Key: s.Key, Label: label, Value: formatStatValue(float64(s.Value))})
			}
			sort.SliceStable(st.Rows, func(i, j int) bool { return st.Rows[i].Label < st.Rows[j].Label })
			o.Stats = append(o.Stats, st)
		}
	}
	return o
}
