package model

import (
	"testing"
	"time"
)

func TestCatchupRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := LoadCatchup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastRun != "" || len(st.Reviewed) != 0 {
		t.Fatalf("missing file should load empty, got %+v", st)
	}

	st.RecordRun(time.Date(2026, 11, 3, 19, 40, 0, 0, SiteLocation))
	st.AddReviewed(ReviewedActivity{Provider: ProviderSteam, ID: "1145360", Title: "Hades", Through: "2019-05-01"})
	if err := SaveCatchup(dir, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCatchup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastRun != st.LastRun || got.ReviewedThrough(ProviderSteam, "1145360") != "2019-05-01" {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.ReviewedThrough(ProviderSteam, "1") != "" {
		t.Error("an unreviewed record must report no through date")
	}
}

// Reviewing is a decision that only ever widens: a later dismiss through an
// earlier day (a stale page, say) must not bring back activity already
// reviewed further.
func TestAddReviewed_NeverLowersThrough(t *testing.T) {
	var st CatchupState
	if !st.AddReviewed(ReviewedActivity{Provider: ProviderPSN, ID: "NPWR1", Through: "2026-10-01"}) {
		t.Fatal("first add should report a change")
	}
	if st.AddReviewed(ReviewedActivity{Provider: ProviderPSN, ID: "NPWR1", Through: "2025-01-01"}) {
		t.Error("an earlier through date must not count as a change")
	}
	if got := st.ReviewedThrough(ProviderPSN, "NPWR1"); got != "2026-10-01" {
		t.Errorf("through = %s, want 2026-10-01", got)
	}
	if !st.AddReviewed(ReviewedActivity{Provider: ProviderPSN, ID: "NPWR1", Through: "2026-11-20"}) {
		t.Error("a later through date should raise it")
	}
	if got := st.ReviewedThrough(ProviderPSN, "NPWR1"); got != "2026-11-20" {
		t.Errorf("through = %s, want 2026-11-20", got)
	}
	if len(st.Reviewed) != 1 {
		t.Errorf("one provider/id must stay one entry, got %d", len(st.Reviewed))
	}
}

func TestRecordRun_ShiftsLastIntoPrevious(t *testing.T) {
	var st CatchupState
	first := time.Date(2026, 11, 3, 12, 0, 0, 0, SiteLocation)
	second := time.Date(2026, 11, 22, 12, 0, 0, 0, SiteLocation)
	st.RecordRun(first)
	if st.PreviousRun != "" {
		t.Errorf("first run has no previous, got %q", st.PreviousRun)
	}
	st.RecordRun(second)
	if !st.PreviousRunTime().Equal(first) || !st.LastRunTime().Equal(second) {
		t.Errorf("previous=%s last=%s", st.PreviousRun, st.LastRun)
	}
}
