package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"go.dalton.dog/gamelog/internal/externalid"
	"go.dalton.dog/gamelog/internal/model"
	"go.dalton.dog/gamelog/internal/mutate"
	"go.dalton.dog/gamelog/internal/providers/retroachievements"
)

// RetroAchievements publishes a bonus achievement set — Professor Layton and
// the Last Specter's "Mouse Alley", say — as a *separate game id* titled
// "<base> [Subset - <name>]". Left alone, every one of those shows up in the
// scan as an unlogged game, and creating it makes a second entry for a game
// that already has one.
//
// So a subset is attached to the game it belongs to instead: its id goes in
// that game's `retroachievements_subsets:`, its history is archived under its
// own id like any other record, and the achievement summary keeps its counts
// on their own line rather than folding them into the base game's (see
// model.AchievementSummary.Subsets).
//
// Two facts shape how this file works, both confirmed against live responses:
//
//   - The bulk endpoint the scan runs on (GetUserCompletionProgress) carries
//     no parent link at all — only the title convention. That is a heuristic,
//     good enough to *spot* a subset and never good enough to link one.
//   - The per-game endpoint (GetGameInfoAndUserProgress) does carry a real
//     ParentGameID. So every link this file writes is confirmed by one extra
//     request against that endpoint, and a mismatch is refused rather than
//     guessed past.

// SubsetInfo is the subset half of a scan candidate: what set it is, and what
// game it belongs under. Everything but Name comes from ResolveSubsets.
type SubsetInfo struct {
	// Name is the subset's own half of RA's title convention ("Mouse Alley").
	Name string
	// BaseTitle names the game it belongs to — RA's own title for the parent
	// once resolved, and before that the base half of the subset's title.
	BaseTitle string
	// ParentID is the base game's RetroAchievements id, from ParentGameID.
	// Blank when the lookup failed; Err says why.
	ParentID string
	// BaseSlug is the logged base game's directory name, blank when the base
	// game has no entry in content/games yet. That's the whole difference
	// between the two actions a subset row offers.
	BaseSlug string
	Err      string
}

// Attachable reports whether the base game is already logged, so this subset
// can simply be attached to it.
func (s SubsetInfo) Attachable() bool { return s.BaseSlug != "" && s.ParentID != "" }

// RAParentLookup is the slice of the RetroAchievements client this file needs:
// one per-game call. An interface so a test can answer it without a live key
// (and without reaching into the provider package's unexported URL vars).
type RAParentLookup interface {
	GetGameProgress(ctx context.Context, gameID string) (retroachievements.RAProgress, error)
}

// RAClientFor builds the lookup client from configured credentials, or nil
// when RetroAchievements isn't configured at all.
func RAClientFor(creds Credentials) RAParentLookup {
	if !creds.RAConfigured() {
		return nil
	}
	return &retroachievements.RAClient{Username: creds.RAUsername, APIKey: creds.RAAPIKey}
}

// raParentCache memoizes subset id -> parent id for the process. A subset's
// parent never changes, and the housekeeping page re-derives its scan on every
// render; at RA's ~1.2s throttle, re-asking per page load would be the slowest
// thing on it.
var raParentCache = struct {
	sync.Mutex
	byID map[string]string
}{byID: map[string]string{}}

// resolveParentID asks RetroAchievements which game a subset belongs to. The
// answer is authoritative — see the package comment above on why the title is
// not.
func resolveParentID(ctx context.Context, ra RAParentLookup, subsetID string) (string, error) {
	raParentCache.Lock()
	cached, ok := raParentCache.byID[subsetID]
	raParentCache.Unlock()
	if ok {
		return cached, nil
	}

	if ra == nil {
		return "", fmt.Errorf("retroachievements credentials not configured")
	}
	progress, err := ra.GetGameProgress(ctx, subsetID)
	if err != nil {
		return "", err
	}
	if progress.ParentGameID == 0 {
		// Titled like a subset, but RA says it stands alone. Believe RA.
		return "", fmt.Errorf("retroachievements reports no parent game for %s", subsetID)
	}
	parent := strconv.Itoa(progress.ParentGameID)

	raParentCache.Lock()
	raParentCache.byID[subsetID] = parent
	raParentCache.Unlock()
	return parent, nil
}

// ResolveSubsets fills in the parent half of every subset candidate: which
// game id it really belongs to, and whether that game is already logged. It
// costs one request per *distinct* subset ever seen this process (see
// raParentCache), and touches nothing else — candidates that aren't subsets
// are returned exactly as they came in.
//
// A failed lookup is recorded on the row rather than returned: one
// unresolvable subset shouldn't cost the whole scan.
func ResolveSubsets(ctx context.Context, ra RAParentLookup, candidates []Candidate, index loggedIndex) []Candidate {
	for i := range candidates {
		c := &candidates[i]
		if c.Subset == nil || c.ProviderKey() != model.ProviderRA {
			continue
		}
		// Copied rather than mutated in place: WithStatus hands out copies of
		// a Candidate, and a shared pointer would let one row's resolution
		// show up on another's.
		info := *c.Subset
		parent, err := resolveParentID(ctx, ra, c.ID)
		if err != nil {
			info.Err = err.Error()
			c.Subset = &info
			continue
		}
		info.ParentID = parent
		if base, ok := index.raGames[parent]; ok {
			info.BaseSlug, info.BaseTitle = base.Slug, base.Title
		}
		c.Subset = &info
	}
	return candidates
}

// AttachSubset links a RetroAchievements subset to an already-logged game:
// writes the id into that game's front matter, captures the subset's unlock
// history into the archive under its own id, and rebuilds the game's
// achievement summary so the new counts show up.
//
// The parent link is verified against the target game's own
// retroachievements_id before anything is written. The id arrives from a
// submitted form, and attaching a subset to the wrong game would put another
// game's achievements on this one's page — a wrong answer that looks entirely
// plausible on screen.
func AttachSubset(ctx context.Context, gamesDir, slug, subsetID string, ra RAParentLookup, creds Credentials) (title string, err error) {
	path := filepath.Join(gamesDir, slug, "_index.md")
	doc, err := model.LoadDoc(path)
	if err != nil {
		return "", err
	}
	raID, _ := doc.ExternalIDs()
	// Front matter is allowed to hold the URL the id was copied from, the same
	// leniency `suggest` gives it.
	if id, err := externalid.ParseExternalID(raID); err == nil {
		raID = id
	}
	if raID == "" {
		return "", fmt.Errorf("%s has no retroachievements_id, so a subset has nothing to hang off", slug)
	}

	if ra == nil {
		return "", fmt.Errorf("retroachievements credentials not configured; see tools/gamelog/README.md")
	}
	progress, err := ra.GetGameProgress(ctx, subsetID)
	if err != nil {
		return "", err
	}
	parent := ""
	if progress.ParentGameID != 0 {
		parent = strconv.Itoa(progress.ParentGameID)
	}
	if parent != raID {
		return "", fmt.Errorf("refusing to attach: retroachievements says %s belongs to game %s, but %s is game %s",
			model.FirstNonEmpty(progress.Title, subsetID), model.FirstNonEmpty(parent, "nothing"), doc.FM.Title, raID)
	}

	if !doc.AddRASubset(subsetID) {
		return progress.Title, errAlreadyAttached
	}
	if err := mutate.WriteFrontMatter(doc, nil, false); err != nil {
		return "", err
	}

	// Capture the subset's own history. A failure here costs the history, not
	// the link — `gamelog achievements <slug>` picks it up on the next run,
	// since the link is what puts it in ProviderLinks. No title is passed: a
	// subset record is titled by RetroAchievements, from the fetch itself.
	archiveDir := model.FindArchiveDir(gamesDir)
	links := []model.ProviderLink{{Provider: model.ProviderRA, ID: subsetID, Subset: true}}
	if _, err := SaveAchievements(ctx, archiveDir, "", links, creds); err != nil {
		return progress.Title, fmt.Errorf("linked, but no achievements saved: %w", err)
	}
	if _, err := model.WriteAchievementSummary(archiveDir, filepath.Dir(path), doc.ProviderLinks()); err != nil {
		return progress.Title, fmt.Errorf("linked, but achievement summary not written: %w", err)
	}
	return progress.Title, nil
}

// errAlreadyAttached is returned when the subset was already linked. It's a
// distinct error so a caller can report it as a no-op rather than a failure:
// the scan re-derives its rows, so the same button is easy to click twice.
var errAlreadyAttached = errors.New("already attached")

// IsAlreadyAttached reports whether an AttachSubset error was the harmless
// "this was already linked" one.
func IsAlreadyAttached(err error) bool { return errors.Is(err, errAlreadyAttached) }

// CreateBaseAndAttach is the subset action for a base game that isn't logged
// at all: create the base game from what RetroAchievements reports about it,
// then attach the subset to it.
//
// The base game's fields come from a fresh per-game fetch rather than from the
// submitted form — the same reasoning as AttachSubset's parent check. It's
// created as a draft with a guessed status, exactly like every other
// scan-created game.
func CreateBaseAndAttach(ctx context.Context, gamesDir, subsetID string, ra RAParentLookup, creds Credentials) (slug, baseTitle string, err error) {
	if ra == nil {
		return "", "", fmt.Errorf("retroachievements credentials not configured; see tools/gamelog/README.md")
	}
	parentID, err := resolveParentID(ctx, ra, subsetID)
	if err != nil {
		return "", "", err
	}
	parent, err := ra.GetGameProgress(ctx, parentID)
	if err != nil {
		return "", "", err
	}
	if parent.Title == "" {
		return "", "", fmt.Errorf("retroachievements returned no title for game %s", parentID)
	}

	base := Candidate{
		Provider:      "RetroAchievements",
		Title:         parent.Title,
		Platform:      parent.ConsoleName,
		ID:            parentID,
		AchievementsA: parent.NumAwardedToUser,
		AchievementsB: parent.NumAchievements,
		Status:        "playing",
	}
	// Same mapping ScanRA applies to the bulk list, on the same fields.
	if suggestion := retroachievements.RASuggestRange(parent); suggestion.OK && suggestion.AwardKind != "" {
		base.Finished = true
		base.AwardKind = suggestion.AwardKind
		base.Status = statusForAward(suggestion.AwardKind)
		base.FinishedOn = day(suggestion.Finished)
	}

	slug = model.Slugify(base.Title)
	// The base game can already exist — it has its own scan row whenever any
	// of its achievements are earned, so it may have been created from a page
	// rendered before this one. Attaching is then the only half left to do,
	// and AttachSubset's parent check is what keeps a slug collision with an
	// unrelated game from quietly becoming an attach to the wrong one.
	if _, statErr := os.Stat(filepath.Join(gamesDir, slug, "_index.md")); statErr != nil {
		if created, _ := CreateFromCandidates(gamesDir, []Candidate{base}, []int{0}, "", creds); len(created) == 0 {
			return "", base.Title, fmt.Errorf("could not create %s", base.Title)
		}
	}

	// A partial failure here is reported as-is rather than as "not attached":
	// AttachSubset writes the link before capturing history, so its own
	// message is the accurate one about which half landed.
	if _, err := AttachSubset(ctx, gamesDir, slug, subsetID, ra, creds); err != nil && !IsAlreadyAttached(err) {
		return slug, base.Title, fmt.Errorf("%s: %w", base.Title, err)
	}
	return slug, base.Title, nil
}
