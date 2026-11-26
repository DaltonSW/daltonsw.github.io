package commands

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.dalton.dog/gamelog/internal/model"
)

// ProviderActivity is one provider record's share of an ActivityItem: what it
// reports that playthroughs.yaml doesn't account for yet.
type ProviderActivity struct {
	Provider string
	ID       string
	Label    string // "Steam", or "RetroAchievements · Mouse Alley" for a subset
	Platform string
	Title    string // the record's own title, carried into a review entry

	Days        int    // distinct uncovered activity days
	First, Last string // earliest/latest uncovered day
	NewUnlocks  int    // unlocks dated on or after the "since" boundary
	LastPlayed  string
	// Latest is the newest activity day this record reports at all, covered
	// or not — what a dismissal marks it reviewed through.
	Latest string
}

// ActivityGuess is the pre-filled answer an activity card offers. Nothing is
// written until it's submitted, and every field is editable first: a gap in
// achievement unlocks marks an activity arc, not a confirmed playthrough.
type ActivityGuess struct {
	// Target is the playthrough index to add a session to, or -1 for a new
	// playthrough.
	Target       int
	Status       string // for a new playthrough
	Started      string
	Finished     string
	FinishedHint string // last activity, shown as the placeholder when Finished is blank
	// Beaten is the day the run beat the game, when an award says so and the
	// run carried on past it (a new playthrough only).
	Beaten string
}

// ActivityItem is one logged game with provider activity on days no logged
// playthrough or session covers.
type ActivityItem struct {
	Game      model.GameSummary
	Providers []ProviderActivity // only the ones with uncovered activity

	Days        int    // distinct uncovered days across every provider
	First, Last string // uncovered range across every provider
	NewUnlocks  int
	// Award is the day of a finishing award (an RA beaten/mastered award, a
	// PSN platinum, every Steam achievement) and AwardKind what it was — only
	// set when that day is itself uncovered, since an award inside a logged
	// run belongs to that run.
	Award, AwardKind string

	// Runs are the game's non-planned playthroughs, the targets a card's
	// "add as session" choice can point at.
	Runs  []model.Playthrough
	Guess ActivityGuess
}

// IsNew reports whether any of the uncovered activity happened on or after
// since — i.e. it's play since the last catch-up, not old history. Compared
// by day, so activity later on the same day as the last run still counts.
func (it ActivityItem) IsNew(since time.Time) bool {
	return since.IsZero() || it.Last >= day(since)
}

// RecordProblem groups archive records whose latest fetch failed the same way
// on the same provider — an expired PSN npsso fails every PSN game at once.
type RecordProblem struct {
	Provider string
	Error    string
	Games    []string
}

// ActivityReport is everything Catch up reads off the archive in one pass.
type ActivityReport struct {
	Items []ActivityItem
	// GamesPlayed and Unlocks summarise what's happened since the boundary:
	// games with any activity day on or after it, and achievements unlocked.
	GamesPlayed int
	Unlocks     int
	// Problems are records whose last attempt, on or after problemsSince,
	// failed. Empty when problemsSince is zero.
	Problems []RecordProblem
}

// ActivityOptions tunes FindUnloggedActivity.
type ActivityOptions struct {
	Reviewed      model.CatchupState // days on or before a record's ReviewedThrough are ignored
	Since         time.Time          // the "new since last catch-up" boundary
	StaleDays     int                // activity older than this guesses a closed run
	ProblemsSince time.Time          // report fetch failures attempted on or after this
	Now           time.Time          // zero means time.Now()
}

// FindUnloggedActivity reads every linked game's archive records — no API
// calls — and returns the games whose provider activity falls on days
// playthroughs.yaml doesn't cover (the same coverage rules as WithCoverage:
// sessions, or a playthrough's own range; an open run covers everything until
// the next logged span starts; planned entries cover nothing).
//
// Activity is every unlocked achievement's date, the record's last_played and
// its award date, on all six achievement providers and RA subsets — unlike the
// game page's live suggest panel, which only asks RetroAchievements and Steam.
func FindUnloggedActivity(archiveDir string, games []model.GameSummary, opts ActivityOptions) (ActivityReport, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	sinceDay := ""
	if !opts.Since.IsZero() {
		sinceDay = day(opts.Since)
	}
	problemsDay := ""
	if !opts.ProblemsSince.IsZero() {
		problemsDay = day(opts.ProblemsSince)
	}
	staleCutoff := day(now.AddDate(0, 0, -opts.StaleDays))

	var rep ActivityReport
	problems := map[[2]string]*RecordProblem{}
	var problemOrder [][2]string

	for _, g := range games {
		links := g.ProviderLinks()
		if len(links) == 0 {
			continue
		}
		pf, err := model.LoadPlaythroughs(filepath.Dir(g.Path))
		if err != nil {
			return rep, err
		}
		spans := loggedSpans(pf)

		item := ActivityItem{Game: g}
		uncovered := map[string]bool{}
		played := false
		for _, link := range links {
			rec, err := model.LoadRecord(archiveDir, link.Provider, link.ID)
			if err != nil {
				return rep, err
			}
			if rec == nil {
				continue
			}
			if problemsDay != "" && rec.LastError != "" && rec.LastAttempt >= problemsDay {
				key := [2]string{link.Provider, rec.LastError}
				p, ok := problems[key]
				if !ok {
					p = &RecordProblem{Provider: link.Provider, Error: rec.LastError}
					problems[key] = p
					problemOrder = append(problemOrder, key)
				}
				p.Games = append(p.Games, g.Title)
			}

			reviewed := opts.Reviewed.ReviewedThrough(link.Provider, link.ID)
			pa := ProviderActivity{
				Provider: link.Provider, ID: link.ID, Label: activityLabel(link, rec),
				Platform: rec.Platform, Title: rec.Title, LastPlayed: rec.LastPlayed,
			}
			seen := map[string]bool{}
			consider := func(d string) {
				if d == "" {
					return
				}
				if d > pa.Latest {
					pa.Latest = d
				}
				if sinceDay != "" && d >= sinceDay {
					played = true
				}
				if d <= reviewed || seen[d] {
					return
				}
				seen[d] = true
				if spans.covers(d) >= 0 {
					return
				}
				pa.Days++
				if pa.First == "" || d < pa.First {
					pa.First = d
				}
				if d > pa.Last {
					pa.Last = d
				}
				uncovered[d] = true
			}
			for _, a := range rec.Achievements {
				if !a.Unlocked {
					continue
				}
				d := activityDay(a.Date)
				if sinceDay != "" && d != "" && d >= sinceDay {
					pa.NewUnlocks++
				}
				consider(d)
			}
			consider(activityDay(rec.LastPlayed))
			if rec.AwardKind != "" {
				award := activityDay(rec.AwardDate)
				consider(award)
				if award != "" && award > reviewed && spans.covers(award) < 0 && award > item.Award {
					item.Award, item.AwardKind = award, rec.AwardKind
				}
			}

			rep.Unlocks += pa.NewUnlocks
			item.NewUnlocks += pa.NewUnlocks
			if pa.Days > 0 {
				item.Providers = append(item.Providers, pa)
			}
		}
		if played {
			rep.GamesPlayed++
		}
		if len(uncovered) == 0 {
			continue
		}
		for d := range uncovered {
			item.Days++
			if item.First == "" || d < item.First {
				item.First = d
			}
			if d > item.Last {
				item.Last = d
			}
		}
		for _, p := range pf.Views() {
			if p.Status != "planned" {
				item.Runs = append(item.Runs, p)
			}
		}
		item.Guess = guessActivity(item, staleCutoff)
		rep.Items = append(rep.Items, item)
	}

	sort.SliceStable(rep.Items, func(i, j int) bool { return rep.Items[i].Last > rep.Items[j].Last })
	for _, key := range problemOrder {
		rep.Problems = append(rep.Problems, *problems[key])
	}
	return rep, nil
}

// guessActivity picks the card's pre-filled answer. In order:
//
//   - a one-shot game (endless/multiplayer/software) with a run of that kind:
//     a session on it, spanning the uncovered activity — the run's sessions
//     list is the record, so a new playthrough would fragment it;
//   - an uncovered finishing award: a new finished playthrough ending on the
//     last activity day (the run's end), with a beaten date when the award is
//     a RetroAchievements beaten award earlier than that — a mastery or
//     platinum date says when the set was cleared, not when the game was;
//   - activity that stopped before staleCutoff: a new playthrough closed on
//     the last activity day as unfinished, the same "it trailed off" default
//     stale triage uses when nothing says otherwise;
//   - otherwise, recent activity: a new open playthrough with the game's
//     usual default status.
func guessActivity(it ActivityItem, staleCutoff string) ActivityGuess {
	g := ActivityGuess{Target: -1, Started: it.First}
	if model.IsOneShot(it.Game.Status) {
		best := -1
		for i, r := range it.Runs {
			if !model.IsOneShot(r.Status) && !(it.Game.Status == "software" && r.Status == "playing") {
				continue
			}
			if best < 0 || r.LatestDate() > it.Runs[best].LatestDate() {
				best = i
			}
		}
		if best >= 0 {
			g.Target = it.Runs[best].Index
			g.Finished = it.Last
			return g
		}
		g.Status = model.DefaultPlaythroughStatus(it.Game.Status)
		if it.Last < staleCutoff {
			g.Finished = it.Last
		} else {
			g.FinishedHint = it.Last
		}
		return g
	}
	switch {
	case it.Award != "":
		g.Status, g.Finished = "finished", it.Last
		if strings.HasPrefix(it.AwardKind, "beaten") && it.Award < it.Last {
			g.Beaten = it.Award
		}
	case it.Last < staleCutoff:
		g.Status, g.Finished = "unfinished", it.Last
	default:
		g.Status, g.FinishedHint = model.DefaultPlaythroughStatus(it.Game.Status), it.Last
	}
	return g
}

// activityLabel names a record on a card: the provider, plus the subset's own
// name for a RetroAchievements subset so two RA rows aren't indistinguishable.
func activityLabel(link model.ProviderLink, rec *model.ArchiveRecord) string {
	label := model.ProviderLabel(link.Provider)
	if link.Subset {
		name := link.ID
		if _, sub, ok := model.SplitRASubsetTitle(rec.Title); ok {
			name = sub
		}
		label += " · " + name
	}
	return label
}

// activityDay turns an archived date — RFC3339 with the site's offset for an
// unlock, or a bare YYYY-MM-DD for last_played and most award dates — into the
// calendar day it fell on in the site's timezone. "" when there's no usable
// date (some Xbox 360 unlocks come back undated).
func activityDay(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return day(t)
	}
	if len(s) >= 10 {
		if _, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return s[:10]
		}
	}
	return ""
}
