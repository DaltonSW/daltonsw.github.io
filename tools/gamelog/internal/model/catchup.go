package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// catchupFilename holds the Catch up page's own state: when "Load what's new"
// last ran, and which provider activity has been reviewed and deliberately
// left unlogged. It lives in archive/ beside ignored.yaml for the same reason:
// it records decisions keyed by provider IDs that never change, so a rename or
// delete in content/ can't disturb it.
//
// Nothing captured lives here, so it's rewritten whole (atomically).
const catchupFilename = "catchup.yaml"

// ReviewedActivity marks one provider record's activity, through a given day,
// as looked at and not worth logging — an old Steam unlock from years before
// the first logged run, say. Catch up stops offering any activity day on or
// before Through for that provider/id; anything newer comes back. Title is for
// the human reading the file.
type ReviewedActivity struct {
	Provider   string `yaml:"provider"`
	ID         string `yaml:"id"`
	Title      string `yaml:"title,omitempty"`
	Through    string `yaml:"through"` // YYYY-MM-DD
	ReviewedOn string `yaml:"reviewed_on,omitempty"`
}

// CatchupState is the whole file.
type CatchupState struct {
	// LastRun is when the most recent "Load what's new" started, and
	// PreviousRun the one before it — the boundary "new since last time" is
	// measured from. RFC3339.
	LastRun     string             `yaml:"last_run,omitempty"`
	PreviousRun string             `yaml:"previous_run,omitempty"`
	Reviewed    []ReviewedActivity `yaml:"reviewed,omitempty"`
}

// CatchupPath is where the state lives for a given archive directory.
func CatchupPath(archiveDir string) string {
	return filepath.Join(archiveDir, catchupFilename)
}

// LoadCatchup reads the state, returning an empty one (not an error) when
// Catch up has never run.
func LoadCatchup(archiveDir string) (CatchupState, error) {
	var st CatchupState
	raw, err := os.ReadFile(CatchupPath(archiveDir))
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if err := yaml.Unmarshal(raw, &st); err != nil {
		return st, fmt.Errorf("%s: %w", CatchupPath(archiveDir), err)
	}
	return st, nil
}

// SaveCatchup writes the state back, reviewed entries sorted by provider then
// id so diffs stay small.
func SaveCatchup(archiveDir string, st CatchupState) error {
	sort.SliceStable(st.Reviewed, func(i, j int) bool {
		a, b := st.Reviewed[i], st.Reviewed[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.ID < b.ID
	})
	body, err := yaml.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	header := "# gamelog Catch up state: when \"Load what's new\" last ran, and provider\n" +
		"# activity reviewed and deliberately left unlogged (through the given day).\n" +
		"# Remove a reviewed entry and that activity is offered again.\n"
	return writeFileAtomic(CatchupPath(archiveDir), append([]byte(header), body...), 0o644)
}

// ReviewedThrough is the day this provider record's activity has been
// reviewed through, or "" when none of it has.
func (st CatchupState) ReviewedThrough(provider, id string) string {
	for _, r := range st.Reviewed {
		if r.Provider == provider && r.ID == id {
			return r.Through
		}
	}
	return ""
}

// AddReviewed folds entries into the reviewed list. A provider/id already on
// it only ever has its Through raised, never lowered — reviewing something
// can't bring back activity that was reviewed further before. Returns whether
// anything changed.
func (st *CatchupState) AddReviewed(entries ...ReviewedActivity) bool {
	today := time.Now().In(SiteLocation).Format("2006-01-02")
	changed := false
	for _, e := range entries {
		if e.Provider == "" || e.ID == "" || e.Through == "" {
			continue
		}
		if e.ReviewedOn == "" {
			e.ReviewedOn = today
		}
		found := false
		for i := range st.Reviewed {
			r := &st.Reviewed[i]
			if r.Provider != e.Provider || r.ID != e.ID {
				continue
			}
			found = true
			if e.Through > r.Through {
				r.Through, r.ReviewedOn = e.Through, e.ReviewedOn
				if e.Title != "" {
					r.Title = e.Title
				}
				changed = true
			}
			break
		}
		if !found {
			st.Reviewed = append(st.Reviewed, e)
			changed = true
		}
	}
	return changed
}

// RecordRun notes that a "Load what's new" run started at now: the run before
// it becomes the boundary "new since last time" is measured from.
func (st *CatchupState) RecordRun(now time.Time) {
	if st.LastRun != "" {
		st.PreviousRun = st.LastRun
	}
	st.LastRun = now.In(SiteLocation).Format(time.RFC3339)
}

// LastRunTime and PreviousRunTime parse the stored stamps, zero when unset or
// unreadable.
func (st CatchupState) LastRunTime() time.Time     { return parseStamp(st.LastRun) }
func (st CatchupState) PreviousRunTime() time.Time { return parseStamp(st.PreviousRun) }

func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
