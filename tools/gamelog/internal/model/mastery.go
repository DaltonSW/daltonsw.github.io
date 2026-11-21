package model

import (
	"sort"
	"strings"
)

// Mastery scopes. A game can be mastered more than once over its life: the
// base game first, then each DLC pack as it's added and cleared.
const (
	// MasteryBase is the base game: a PSN platinum, a PSN "default" trophy
	// group at 100%, or a RetroAchievements mastered/completed award.
	MasteryBase = "base"
	// MasteryAll is every achievement the provider listed at the time, on any
	// provider. When DLC later raises the total and it's cleared again, that's
	// a second MasteryAll milestone with a larger Unlocked.
	MasteryAll = "all"
	// Any other Scope is a PSN trophy group id ("001", "002", …) — one DLC pack.
)

// MasteryMilestone is one moment a set of achievements was completed.
type MasteryMilestone struct {
	Scope string `json:"scope" yaml:"scope"`
	// Date is the day of the unlock (or award) that completed the scope.
	Date string `json:"date" yaml:"date"`
	// Unlocked is the size of the scope when it was completed — what tells a
	// 50/50 mastery from a later 60/60 one after DLC.
	Unlocked int `json:"unlocked" yaml:"unlocked"`
	// Basis is what the milestone was read from: "platinum", "ra-mastered",
	// "ra-completed" or "100%".
	Basis string `json:"basis" yaml:"basis"`
}

// DeriveMastery reads the milestones a record qualifies for right now. It is
// deliberately provider-agnostic: it keys off Tier, Group and AwardKind,
// which only the providers that have those concepts ever set.
func DeriveMastery(p *ProviderRecord) []MasteryMilestone {
	if p == nil {
		return nil
	}
	var out []MasteryMilestone

	// Per trophy group (PSN only). "default" is the base game.
	type groupStat struct {
		size, unlocked int
		last           string
	}
	groups := map[string]*groupStat{}
	var platinumDate, platinumGroup string
	for _, a := range p.Achievements {
		if a.Tier == "platinum" && a.Unlocked && a.Date != "" {
			platinumDate, platinumGroup = day10(a.Date), FirstNonEmpty(a.Group, "default")
		}
		if a.Group == "" {
			continue
		}
		g := groups[a.Group]
		if g == nil {
			g = &groupStat{}
			groups[a.Group] = g
		}
		g.size++
		if a.Unlocked && a.Date != "" {
			g.unlocked++
			if d := day10(a.Date); d > g.last {
				g.last = d
			}
		}
	}

	switch {
	case platinumDate != "":
		size := len(p.Achievements)
		if g := groups[platinumGroup]; g != nil {
			size = g.size
		}
		out = append(out, MasteryMilestone{Scope: MasteryBase, Date: platinumDate, Unlocked: size, Basis: "platinum"})
	case groups["default"] != nil && groups["default"].size == groups["default"].unlocked:
		g := groups["default"]
		out = append(out, MasteryMilestone{Scope: MasteryBase, Date: g.last, Unlocked: g.size, Basis: "100%"})
	}
	for id, g := range groups {
		if id == "default" || g.size == 0 || g.unlocked != g.size {
			continue
		}
		out = append(out, MasteryMilestone{Scope: id, Date: g.last, Unlocked: g.size, Basis: "100%"})
	}

	// RetroAchievements' award. HighestAwardKind is only the highest, so a
	// "beaten-*" here says nothing about mastery either way.
	if kind := strings.ToLower(p.AwardKind); (kind == "mastered" || kind == "completed") && p.AwardDate != "" {
		out = append(out, MasteryMilestone{Scope: MasteryBase, Date: day10(p.AwardDate), Unlocked: p.Total, Basis: "ra-" + kind})
	}

	// Every listed achievement, any provider. Summarize only counts an unlock
	// that has a date, so Unlocked == Total guarantees Last is real.
	if p.Total > 0 && p.Unlocked >= p.Total && p.Last != "" {
		out = append(out, MasteryMilestone{Scope: MasteryAll, Date: day10(p.Last), Unlocked: p.Total, Basis: "100%"})
	}
	return out
}

// unionMastery merges milestone lists, keeping the earliest date seen for
// each one. A base-game mastery happens once, so "base" is keyed on its scope
// alone; every other scope also keys on its size, which is what keeps a 50/50
// mastery when a DLC later makes it 60/60.
func unionMastery(lists ...[]MasteryMilestone) []MasteryMilestone {
	key := func(m MasteryMilestone) [2]any {
		if m.Scope == MasteryBase {
			return [2]any{m.Scope, 0}
		}
		return [2]any{m.Scope, m.Unlocked}
	}
	byKey := map[[2]any]MasteryMilestone{}
	for _, list := range lists {
		for _, m := range list {
			if m.Date == "" {
				continue
			}
			k := key(m)
			if prev, ok := byKey[k]; !ok || m.Date < prev.Date {
				byKey[k] = m
			}
		}
	}
	if len(byKey) == 0 {
		return nil
	}
	out := make([]MasteryMilestone, 0, len(byKey))
	for _, m := range byKey {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		if out[i].Scope != out[j].Scope {
			return scopeRank(out[i].Scope) < scopeRank(out[j].Scope)
		}
		return out[i].Unlocked < out[j].Unlocked
	})
	return out
}

// scopeRank orders same-day milestones base → DLC groups → all.
func scopeRank(scope string) string {
	switch scope {
	case MasteryBase:
		return "0"
	case MasteryAll:
		return "2"
	}
	return "1" + scope
}

// MasteredOn is the day a game first counts as mastered, given its records'
// milestones: the earliest base-game or full-set milestone. DLC-group
// milestones alone don't make a game mastered.
func MasteredOn(milestones []MasteryMilestone) string {
	var first string
	for _, m := range milestones {
		if m.Scope != MasteryBase && m.Scope != MasteryAll {
			continue
		}
		if first == "" || m.Date < first {
			first = m.Date
		}
	}
	return first
}

func day10(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
