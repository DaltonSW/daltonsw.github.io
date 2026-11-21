package model

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func trophy(key, group, tier, date string) ArchivedAchievement {
	a := ArchivedAchievement{Key: key, Name: key, Group: group, Tier: tier}
	if date != "" {
		a.Unlocked, a.Date = true, date
	}
	return a
}

func milestone(scope, date string, n int, basis string) MasteryMilestone {
	return MasteryMilestone{Scope: scope, Date: date, Unlocked: n, Basis: basis}
}

// Bloodborne's shape: platinum for the base game, then the DLC trophy group
// cleared a year later. Base and DLC mastery are separate milestones.
func TestDeriveMastery_PSNPlatinumAndDLCGroup(t *testing.T) {
	p := &ProviderRecord{Achievements: []ArchivedAchievement{
		trophy("0", "default", "platinum", "2020-12-12T20:00:00-06:00"),
		trophy("1", "default", "gold", "2020-12-12T19:59:00-06:00"),
		trophy("2", "default", "bronze", "2020-06-18T21:01:25-05:00"),
		trophy("3", "001", "gold", "2021-11-13T18:00:00-06:00"),
		trophy("4", "001", "bronze", "2021-10-01T18:00:00-05:00"),
	}}
	p.Summarize()

	want := []MasteryMilestone{
		milestone(MasteryBase, "2020-12-12", 3, "platinum"),
		milestone("001", "2021-11-13", 2, "100%"),
		milestone(MasteryAll, "2021-11-13", 5, "100%"),
	}
	if !reflect.DeepEqual(p.Mastery, want) {
		t.Errorf("Mastery =\n %+v\nwant\n %+v", p.Mastery, want)
	}
	if got := MasteredOn(p.Mastery); got != "2020-12-12" {
		t.Errorf("MasteredOn = %q, want the platinum date", got)
	}
}

// KH3's shape: platinum earned, DLC trophies not. Mastered, but not "all".
func TestDeriveMastery_PlatinumWithoutDLCIsStillMastered(t *testing.T) {
	p := &ProviderRecord{Achievements: []ArchivedAchievement{
		trophy("0", "default", "platinum", "2022-07-23T10:00:00-05:00"),
		trophy("1", "default", "gold", "2022-07-23T09:00:00-05:00"),
		trophy("2", "001", "bronze", ""),
	}}
	p.Summarize()
	want := []MasteryMilestone{milestone(MasteryBase, "2022-07-23", 2, "platinum")}
	if !reflect.DeepEqual(p.Mastery, want) {
		t.Errorf("Mastery = %+v, want %+v", p.Mastery, want)
	}
}

func TestDeriveMastery_RAAwardOnlyCountsMastery(t *testing.T) {
	beaten := &ProviderRecord{Total: 3, AwardKind: "beaten-hardcore", AwardDate: "2026-01-01",
		Achievements: []ArchivedAchievement{unlocked("a", "2026-01-01T10:00:00-06:00"), locked("b"), locked("c")}}
	beaten.Summarize()
	if beaten.Mastery != nil {
		t.Errorf("a beaten award is not mastery, got %+v", beaten.Mastery)
	}

	mastered := &ProviderRecord{Total: 2, AwardKind: "mastered", AwardDate: "2026-03-20",
		Achievements: []ArchivedAchievement{unlocked("a", "2026-03-01T10:00:00-06:00"), unlocked("b", "2026-03-20T10:00:00-05:00")}}
	mastered.Summarize()
	want := []MasteryMilestone{
		milestone(MasteryBase, "2026-03-20", 2, "ra-mastered"),
		milestone(MasteryAll, "2026-03-20", 2, "100%"),
	}
	if !reflect.DeepEqual(mastered.Mastery, want) {
		t.Errorf("Mastery = %+v, want %+v", mastered.Mastery, want)
	}
}

// The data-loss case this exists for: a game mastered at 2/2 gets a DLC that
// adds a third achievement. The fetch that brings the DLC in must not erase
// the original mastery — even for a record written before milestones existed.
func TestMergeProvider_DLCNeverErasesAMastery(t *testing.T) {
	old := &ProviderRecord{ID: "1", Total: 2, Achievements: []ArchivedAchievement{
		unlocked("a", "2024-01-01T10:00:00-06:00"),
		unlocked("b", "2024-01-05T10:00:00-06:00"),
	}}
	old.Summarize()
	old.Mastery = nil // as written by a version of the tool that predates milestones

	fresh := &ProviderRecord{ID: "1", Total: 3, Fetched: "2025-06-01", Achievements: []ArchivedAchievement{
		unlocked("a", "2024-01-01T10:00:00-06:00"),
		unlocked("b", "2024-01-05T10:00:00-06:00"),
		locked("dlc"),
	}}
	fresh.Summarize()
	merged := mergeProvider(old, fresh)
	want := []MasteryMilestone{milestone(MasteryAll, "2024-01-05", 2, "100%")}
	if !reflect.DeepEqual(merged.Mastery, want) {
		t.Fatalf("after DLC: Mastery = %+v, want %+v", merged.Mastery, want)
	}

	// Clearing the DLC later is a second milestone, not a replacement.
	cleared := &ProviderRecord{ID: "1", Total: 3, Fetched: "2025-07-01", Achievements: []ArchivedAchievement{
		unlocked("a", "2024-01-01T10:00:00-06:00"),
		unlocked("b", "2024-01-05T10:00:00-06:00"),
		unlocked("dlc", "2025-06-30T10:00:00-05:00"),
	}}
	cleared.Summarize()
	merged = mergeProvider(merged, cleared)
	want = append(want, milestone(MasteryAll, "2025-06-30", 3, "100%"))
	if !reflect.DeepEqual(merged.Mastery, want) {
		t.Errorf("after clearing DLC: Mastery = %+v, want %+v", merged.Mastery, want)
	}
	if got := MasteredOn(merged.Mastery); got != "2024-01-05" {
		t.Errorf("MasteredOn = %q, want the first mastery", got)
	}
}

// An RA set revision can demote HighestAwardKind; the award milestone stays.
func TestMergeProvider_KeepsAnAwardTheProviderNoLongerReports(t *testing.T) {
	old := &ProviderRecord{ID: "1", Total: 1, AwardKind: "mastered", AwardDate: "2026-03-20",
		Achievements: []ArchivedAchievement{unlocked("a", "2026-03-20T10:00:00-05:00")}}
	old.Summarize()
	fresh := &ProviderRecord{ID: "1", Total: 2, AwardKind: "beaten-hardcore", AwardDate: "2026-03-20", Fetched: "2026-05-01",
		Achievements: []ArchivedAchievement{unlocked("a", "2026-03-20T10:00:00-05:00"), locked("b")}}
	merged := mergeProvider(old, fresh)
	if MasteredOn(merged.Mastery) != "2026-03-20" {
		t.Errorf("award milestone lost: %+v", merged.Mastery)
	}
}

func TestUnionMastery_KeepsEarliestDate(t *testing.T) {
	got := unionMastery(
		[]MasteryMilestone{milestone(MasteryBase, "2021-01-01", 40, "platinum")},
		[]MasteryMilestone{milestone(MasteryBase, "2020-12-12", 34, "platinum"), milestone(MasteryAll, "", 40, "100%")},
	)
	want := []MasteryMilestone{milestone(MasteryBase, "2020-12-12", 34, "platinum")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Sekiro's shape: mastered on one of two providers is mastered.
func TestWriteAchievementSummary_MasteredOnAnyProvider(t *testing.T) {
	archiveDir, gameDir := t.TempDir(), t.TempDir()
	if _, err := SaveRecord(archiveDir, ProviderSteam, "1", "G", &ProviderRecord{ID: "1", Total: 1, Fetched: "x",
		Achievements: []ArchivedAchievement{unlocked("a", "2024-07-13T10:00:00-05:00")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRecord(archiveDir, ProviderPSN, "2", "G", &ProviderRecord{ID: "2", Total: 2, Fetched: "x",
		Achievements: []ArchivedAchievement{unlocked("a", "2025-04-16T10:00:00-05:00"), locked("b")}}); err != nil {
		t.Fatal(err)
	}
	links := []ProviderLink{{Provider: ProviderSteam, ID: "1"}, {Provider: ProviderPSN, ID: "2"}}
	if _, err := WriteAchievementSummary(archiveDir, gameDir, links); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(gameDir, achievementSummaryFilename))
	if err != nil {
		t.Fatal(err)
	}
	var s AchievementSummary
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Mastered != "2024-07-13" {
		t.Errorf("Mastered = %q, want 2024-07-13", s.Mastered)
	}
	if len(s.Providers[0].Mastery) != 1 || len(s.Providers[1].Mastery) != 0 {
		t.Errorf("per-provider milestones wrong: %+v", s.Providers)
	}
}
