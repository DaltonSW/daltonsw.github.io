package model

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlaythroughsFilename is the tool-owned file beside a game's _index.md.
// Hugo reads it as a page resource of the game's bundle, so it travels with
// the game and disappears with it — unlike the achievement archive, which is
// deliberately kept outside content/ so a display decision can't delete it.
const PlaythroughsFilename = "playthroughs.yaml"

// generatedBanner is re-emitted on every write. This file is encoded whole
// rather than patched, so any comment a human adds is lost on the next write
// — say so in the file itself rather than letting someone find out.
const generatedBanner = `# Written by tools/gamelog. Safe to edit by hand, but comments are not
# preserved: the tool rewrites this file whole. Prose belongs in _index.md.
`

// SessionEntry is one date range within a playthrough, for a game picked up
// and put down repeatedly.
//
// `finished` is deliberately not omitempty: an ongoing session keeps the key
// with an empty value, which reads better than the field vanishing and keeps
// the shape stable for anything diffing this file.
type SessionEntry struct {
	Started  string `yaml:"started"`
	Finished string `yaml:"finished"`
	Title    string `yaml:"title,omitempty"`

	// Extra preserves keys this tool doesn't know about. Without it a
	// whole-file rewrite would silently drop any field added by hand or by a
	// later version of the tool — the same class of data loss the old
	// line-splice editor had, just via a different mechanism.
	Extra map[string]any `yaml:",inline"`
}

// PlaythroughEntry is one run through a game.
//
// Started/Finished are omitempty here because they legitimately disappear:
// converting a playthrough to `sessions:` moves the pair into the list.
//
// Platform is per-playthrough because the same game is genuinely played on
// more than one of them — Persona 5 Royal on PS4 and again on Steam, Octopath
// Traveler finished on PC and dropped on Switch. Those are separate runs with
// separate dates and separate outcomes, so the platform belongs to the run,
// not to the game. It is omitempty and falls back to the game's front-matter
// `platform:` when absent, which is why every file written before this field
// existed still means what it did.
type PlaythroughEntry struct {
	Started  string         `yaml:"started,omitempty"`
	Finished string         `yaml:"finished,omitempty"`
	Status   string         `yaml:"status,omitempty"`
	Platform string         `yaml:"platform,omitempty"`
	Rating   any            `yaml:"rating,omitempty"`
	Notes    string         `yaml:"notes,omitempty"`
	Sessions []SessionEntry `yaml:"sessions,omitempty"`

	// Subgame names which member of the game's front-matter `subgames:`
	// roster this run covers — e.g. "Plague of Shadows" on a Shovel Knight:
	// Treasure Trove entry. Blank means "the whole game," not "unknown":
	// unlike Platform there is no EffectivePlatform-style fallback, since a
	// game with no declared roster has nothing to inherit from.
	Subgame string `yaml:"subgame,omitempty"`

	Extra map[string]any `yaml:",inline"`
}

// PlaythroughFields holds the values a form collects for one new entry. It
// stays all-strings because that's what the TUI edits in; SetRating converts.
type PlaythroughFields struct {
	Started  string
	Finished string
	Status   string // playing|finished|mastered|dropped|unfinished|paused|endless|multiplayer
	Platform string // blank means "same as the game's front matter"
	Subgame  string // blank means "the whole game"; otherwise a member of the game's subgames: roster
	Rating   string // 1-10 or ""
	Notes    string
}

// PlaythroughsFile is a game's whole playthroughs.yaml.
type PlaythroughsFile struct {
	Path         string             `yaml:"-"`
	Playthroughs []PlaythroughEntry `yaml:"playthroughs"`

	// raw is the file exactly as it was read, kept so a pending write can be
	// checked against what is really on disk. Comparing against the decoded
	// struct instead would miss anything already lost on the way in.
	raw []byte
}

// RatingString renders the rating as the form the TUI edits. YAML decodes a
// bare `rating: 9` as an int, so it can't simply be typed as a string.
func (e PlaythroughEntry) RatingString() string { return scalarString(e.Rating) }

// SetRating stores a rating typed as text, as a number when it is one, so the
// file keeps `rating: 9` rather than `rating: "9"`.
func (e *PlaythroughEntry) SetRating(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		e.Rating = nil
		return
	}
	if n, err := strconv.Atoi(s); err == nil {
		e.Rating = n
		return
	}
	e.Rating = s
}

// PlaythroughsPath is where a game's playthroughs live.
func PlaythroughsPath(gameDir string) string {
	return filepath.Join(gameDir, PlaythroughsFilename)
}

// LoadPlaythroughs reads a game's playthroughs. A game with none has no file
// at all, which is not an error.
func LoadPlaythroughs(gameDir string) (*PlaythroughsFile, error) {
	path := PlaythroughsPath(gameDir)
	f := &PlaythroughsFile{Path: path}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(raw, f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.Path = path
	f.raw = raw
	return f, nil
}

// Raw is the file as it was read, empty for a game that has none yet.
func (f *PlaythroughsFile) Raw() []byte { return f.raw }

// Encode renders the file the way Save would write it.
func (f *PlaythroughsFile) Encode() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(generatedBanner)
	enc := yaml.NewEncoder(&buf)
	// Two-space indent matches how these entries were written when they lived
	// in front matter; yaml.v3 defaults to four.
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save writes the file atomically, or removes it when the last playthrough
// goes away. Atomically because this is now a whole-file rewrite of the only
// copy of this data: a crash partway through must not be able to truncate it.
func (f *PlaythroughsFile) Save() error {
	if len(f.Playthroughs) == 0 {
		if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	out, err := f.Encode()
	if err != nil {
		return err
	}
	return writeFileAtomic(f.Path, out, 0o644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gamelog-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename has succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Session is a read-only view of one session, as the TUI shows it.
type Session struct {
	Index    int
	Started  string
	Finished string
	Title    string
}

// Playthrough is a read-only view of one entry with every field as text,
// which is the shape the forms edit in.
type Playthrough struct {
	Index    int
	Started  string
	Finished string
	Status   string
	Platform string
	Subgame  string
	Rating   string
	Notes    string
	Sessions []Session
}

func (p Playthrough) HasSessions() bool { return len(p.Sessions) > 0 }

// LatestDate is the most recent date this playthrough is known to have
// touched — the last session's end (or start, if that session is still
// open), or the flat started/finished pair for an entry that predates
// sessions entirely. Used to tell whether freshly fetched provider activity
// is already reflected here or represents a session nothing has logged yet.
func (p Playthrough) LatestDate() string {
	if p.HasSessions() {
		last := p.Sessions[len(p.Sessions)-1]
		return FirstNonEmpty(last.Finished, last.Started)
	}
	return FirstNonEmpty(p.Finished, p.Started)
}

// IsOpen reports whether this playthrough (or, if it has sessions, its most
// recent session) has a blank `finished` field.
func (p Playthrough) IsOpen() bool {
	if p.HasSessions() {
		return p.Sessions[len(p.Sessions)-1].Finished == ""
	}
	return p.Finished == ""
}

// EarliestDate is the first date this playthrough is known to have started —
// the minimum of every session's Started once it has any, since sessions
// aren't guaranteed to be logged in chronological order, or the entry's own
// Started for one that predates sessions entirely.
func (p Playthrough) EarliestDate() string {
	if !p.HasSessions() {
		return p.Started
	}
	earliest := ""
	for _, s := range p.Sessions {
		if s.Started != "" && (earliest == "" || s.Started < earliest) {
			earliest = s.Started
		}
	}
	return earliest
}

// gameDateRange folds EarliestDate/LatestDate across every playthrough a
// game has logged, mirroring layouts/partials/game-first-played.html and
// game-last-played.html — the site itself never reads a game's front-matter
// started/finished once anything is logged in playthroughs.yaml, since that's
// where the real per-run dates live (front matter's own started/finished is
// largely a vestige of games created before playthroughs.yaml existed, or of
// gamelog scan, which only ever fills in Finished, never Started). Returns
// "", "" when nothing is logged, so the caller can fall back to front matter
// the same way those partials do.
func gameDateRange(views []Playthrough) (earliest, latest string) {
	for _, p := range views {
		if d := p.EarliestDate(); d != "" && (earliest == "" || d < earliest) {
			earliest = d
		}
		if d := p.LatestDate(); d != "" && d > latest {
			latest = d
		}
	}
	return earliest, latest
}

// Views renders every entry in the form the TUI consumes.
func (f *PlaythroughsFile) Views() []Playthrough {
	out := make([]Playthrough, 0, len(f.Playthroughs))
	for i, e := range f.Playthroughs {
		p := Playthrough{
			Index:    i,
			Started:  e.Started,
			Finished: e.Finished,
			Status:   e.Status,
			Platform: e.Platform,
			Subgame:  e.Subgame,
			Rating:   e.RatingString(),
			Notes:    e.Notes,
		}
		for j, s := range e.Sessions {
			p.Sessions = append(p.Sessions, Session{Index: j, Started: s.Started, Finished: s.Finished, Title: s.Title})
		}
		out = append(out, p)
	}
	return out
}

// AddPlaythrough appends a new entry.
func (f *PlaythroughsFile) AddPlaythrough(pf PlaythroughFields) {
	e := PlaythroughEntry{
		Started:  pf.Started,
		Finished: pf.Finished,
		Status:   pf.Status,
		Platform: pf.Platform,
		Subgame:  pf.Subgame,
		Notes:    pf.Notes,
	}
	e.SetRating(pf.Rating)
	f.Playthroughs = append(f.Playthroughs, e)
}

// AddSession logs another date range against an existing playthrough,
// converting a flat started/finished pair into a `sessions:` list the first
// time one is needed.
//
// This is the operation that used to destroy data. As a line splice it had to
// locate and rewrite two possibly non-adjacent ranges without disturbing
// anything between them; here it moves two strings into a slice.
func (f *PlaythroughsFile) AddSession(idx int, started, finished, title string) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to add a session to", idx+1)
	}
	e := &f.Playthroughs[idx]

	if len(e.Sessions) == 0 {
		if e.Started == "" {
			return fmt.Errorf("playthrough %d has no start date to convert into a session", idx+1)
		}
		e.Sessions = []SessionEntry{{Started: e.Started, Finished: e.Finished}}
		e.Started, e.Finished = "", ""
	}
	e.Sessions = append(e.Sessions, SessionEntry{Started: started, Finished: finished, Title: title})
	return nil
}

// RemoveSession deletes session j from playthrough idx, shifting later
// sessions down. Refuses to leave zero sessions — there is no flat
// started/finished to fall back to once an entry has been converted.
func (f *PlaythroughsFile) RemoveSession(idx, j int) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to remove a session from", idx+1)
	}
	e := &f.Playthroughs[idx]
	if j < 0 || j >= len(e.Sessions) {
		return fmt.Errorf("no session %d on playthrough %d", j+1, idx+1)
	}
	if len(e.Sessions) < 2 {
		return fmt.Errorf("playthrough %d has only one session — delete the playthrough instead", idx+1)
	}
	e.Sessions = append(e.Sessions[:j], e.Sessions[j+1:]...)
	return nil
}

// MoveSession swaps sessions[p] and sessions[p+1] within playthrough idx —
// the only way to reorder sessions once logged, since there's otherwise no
// way to insert a session between two others already on record short of
// deleting and re-adding (which would lose whichever fields Extra was
// carrying). p is the earlier of the pair; p+1 must also be in range.
func (f *PlaythroughsFile) MoveSession(idx, p int) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to reorder sessions on", idx+1)
	}
	e := &f.Playthroughs[idx]
	if p < 0 || p+1 >= len(e.Sessions) {
		return fmt.Errorf("no adjacent session to swap with on playthrough %d", idx+1)
	}
	e.Sessions[p], e.Sessions[p+1] = e.Sessions[p+1], e.Sessions[p]
	return nil
}

// EditSession applies edited started/finished/title to session j in place.
// Editing session 0 of an entry that has never been converted (still a flat
// started/finished pair) performs that conversion in place — the UI shows
// that pair as an implicit session 1 before any explicit sessions: list
// exists, so saving edits to it needs to land the same way AddSession's
// first-time conversion does.
func (f *PlaythroughsFile) EditSession(idx, j int, started, finished, title string) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to edit a session on", idx+1)
	}
	e := &f.Playthroughs[idx]
	if j == 0 && len(e.Sessions) == 0 {
		if e.Started == "" {
			return fmt.Errorf("no session %d on playthrough %d", j+1, idx+1)
		}
		e.Sessions = []SessionEntry{{}}
		e.Started, e.Finished = "", ""
	}
	if j < 0 || j >= len(e.Sessions) {
		return fmt.Errorf("no session %d on playthrough %d", j+1, idx+1)
	}
	e.Sessions[j].Started = started
	e.Sessions[j].Finished = finished
	e.Sessions[j].Title = title
	return nil
}

// SplitPlaythrough moves sessions[j:] out of playthrough idx into a new
// entry appended to the file. append always lands the new entry at the true
// end of f.Playthroughs, so no other entry's index is ever disturbed. The
// new entry inherits idx's Platform and Subgame — a status split over time
// is still the same run of the same subgame; Notes/Rating start blank;
// newStatus is required, never copied from the source. j must be >0.
func (f *PlaythroughsFile) SplitPlaythrough(idx, j int, newStatus string) (int, error) {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return 0, fmt.Errorf("no playthrough %d to split", idx+1)
	}
	e := &f.Playthroughs[idx]
	if j <= 0 || j >= len(e.Sessions) {
		return 0, fmt.Errorf("no session %d to split playthrough %d at", j+1, idx+1)
	}

	moved := append([]SessionEntry(nil), e.Sessions[j:]...)
	e.Sessions = e.Sessions[:j]

	f.Playthroughs = append(f.Playthroughs, PlaythroughEntry{
		Status:   newStatus,
		Platform: e.Platform,
		Subgame:  e.Subgame,
		Sessions: moved,
	})
	return len(f.Playthroughs) - 1, nil
}

// GraduatePlannedPlaythrough turns a bare "planned" placeholder into a real
// entry, in place — idx must not yet have a Started date, so every field
// this sets was previously empty. That makes it a pure addition as far as
// the loss-check is concerned, unlike removing a playthrough entirely (which
// this file deliberately has no operation for — see the package comment on
// why reindexing playthroughs[] is avoided).
func (f *PlaythroughsFile) GraduatePlannedPlaythrough(idx int, pf PlaythroughFields) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to start", idx+1)
	}
	e := &f.Playthroughs[idx]
	if e.Status != "planned" || e.Started != "" {
		return fmt.Errorf("playthrough %d is not a planned placeholder", idx+1)
	}
	e.Started = pf.Started
	e.Finished = pf.Finished
	e.Status = pf.Status
	e.Platform = pf.Platform
	e.Subgame = pf.Subgame
	e.SetRating(pf.Rating)
	e.Notes = pf.Notes
	return nil
}

// EditPlanned updates a planned-replay placeholder's platform/subgame/notes
// in place. Status and dates are untouched here — those only ever change via
// GraduatePlannedPlaythrough, which is the one path that turns "planned"
// into something else.
func (f *PlaythroughsFile) EditPlanned(idx int, platform, subgame, notes string) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to edit", idx+1)
	}
	e := &f.Playthroughs[idx]
	if e.Status != "planned" {
		return fmt.Errorf("playthrough %d is not a planned placeholder", idx+1)
	}
	e.Platform = platform
	e.Subgame = subgame
	e.Notes = notes
	return nil
}

// sessionFieldNames returns the yaml field names one SessionEntry encodes on
// its own: started/finished always, title/extra keys only when present.
func sessionFieldNames(s SessionEntry) map[string]bool {
	names := map[string]bool{"started": true, "finished": true}
	if s.Title != "" {
		names["title"] = true
	}
	for k := range s.Extra {
		names[k] = true
	}
	return names
}

// RemoveSessionAllowedPaths returns the paths that legitimately vanish
// deleting session j from a pre-mutation slice: a field the shifted-in
// session doesn't share with the one it replaced disappears from that path,
// and the old tail slot is gone entirely.
func RemoveSessionAllowedPaths(idx int, sessions []SessionEntry, j int) []string {
	n := len(sessions)
	var paths []string
	for p := j; p <= n-2; p++ {
		cur, next := sessionFieldNames(sessions[p]), sessionFieldNames(sessions[p+1])
		for name := range cur {
			if !next[name] {
				paths = append(paths, SessionPath(idx, p, name))
			}
		}
	}
	for name := range sessionFieldNames(sessions[n-1]) {
		paths = append(paths, SessionPath(idx, n-1, name))
	}
	return paths
}

// MoveSessionAllowedPaths returns the paths that legitimately vanish when
// sessions[p] and sessions[p+1] swap places: a field one side had and the
// other didn't (title, or an Extra key) moves to the other index along with
// the rest of its session, so the path that used to hold it goes quiet
// rather than keeping its old meaning.
func MoveSessionAllowedPaths(idx int, before []SessionEntry, p int) []string {
	a, b := sessionFieldNames(before[p]), sessionFieldNames(before[p+1])
	var paths []string
	for name := range a {
		if !b[name] {
			paths = append(paths, SessionPath(idx, p, name))
		}
	}
	for name := range b {
		if !a[name] {
			paths = append(paths, SessionPath(idx, p+1, name))
		}
	}
	return paths
}

// TruncateSessionAllowedPaths returns the paths that vanish from playthrough
// idx when sessions[from:] is cut away entirely (the split case).
func TruncateSessionAllowedPaths(idx int, sessions []SessionEntry, from int) []string {
	var paths []string
	for p := from; p < len(sessions); p++ {
		for name := range sessionFieldNames(sessions[p]) {
			paths = append(paths, SessionPath(idx, p, name))
		}
	}
	return paths
}

func SessionPath(idx, j int, field string) string {
	return fmt.Sprintf("playthroughs[%d].sessions[%d].%s", idx, j, field)
}

// SyncStatus keeps a game's sole playthrough entry in step with a
// front-matter status change to finished/dropped/mastered/unfinished/paused —
// playthroughs.yaml is what actually renders once any playthrough exists,
// and "ongoing" comes from the entry's finished date, not status text, so
// front matter alone can't fix it. Only acts on exactly one playthrough
// (none: nothing to sync; more than one: ambiguous, use "Update a
// playthrough" instead). Never overwrites an already-set finished date, and
// a blank closedOn (as "paused" should always pass) only syncs the status,
// leaving dates untouched. Safe to call unconditionally.
//
// The web game-info form (server_games.go's handleEditInfo/handleQuickEditGame)
// now gates whether it even reads a Finished value from the request behind
// GameSummary.FinishedEditable, which requires this same "exactly one
// playthrough, still open" condition to be true. So at those two call sites,
// closedOn is only ever non-blank when this method would've backfilled it
// anyway; the meaningful callers of the backfill behavior are
// server_bulk.go's handleStaleAction and the CLI paths through mutate.go,
// not the web form.
func (f *PlaythroughsFile) SyncStatus(status, closedOn string) bool {
	switch status {
	case "finished", "dropped", "mastered", "unfinished", "paused":
	default:
		return false
	}
	if len(f.Playthroughs) != 1 {
		return false
	}
	e := &f.Playthroughs[0]
	changed := false
	if e.Status != status {
		e.Status = status
		changed = true
	}
	if closedOn == "" {
		return changed
	}
	if len(e.Sessions) > 0 {
		last := &e.Sessions[len(e.Sessions)-1]
		if last.Finished == "" {
			last.Finished = closedOn
			changed = true
		}
	} else if e.Finished == "" {
		e.Finished = closedOn
		changed = true
	}
	return changed
}

// UpdatePlaythrough applies edited field values. The finished date belongs to
// the last session once an entry has sessions, matching where the TUI showed
// it.
func (f *PlaythroughsFile) UpdatePlaythrough(idx int, finished, status, platform, subgame, rating, notes string) error {
	if idx < 0 || idx >= len(f.Playthroughs) {
		return fmt.Errorf("no playthrough %d to update", idx+1)
	}
	e := &f.Playthroughs[idx]

	if len(e.Sessions) > 0 {
		e.Sessions[len(e.Sessions)-1].Finished = finished
	} else {
		e.Finished = finished
	}
	e.Status = status
	e.Platform = platform
	e.Subgame = subgame
	e.SetRating(rating)
	e.Notes = notes
	return nil
}

// CollectYAMLFields flattens a YAML document to "path -> value" for every
// scalar, e.g. "playthroughs[0].sessions[1].finished". Comparing the inventory
// of a file against the inventory of a pending rewrite is how a write proves
// it isn't dropping a field it was never asked to touch.
//
// Only presence is meaningful here, not the values: an unquoted date decodes
// to time.Time while the re-encoded form is a quoted string, so the two sides
// legitimately disagree on representation.
func CollectYAMLFields(raw []byte) (map[string]string, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := map[string]string{}
	collectAny(doc, "", out)
	return out, nil
}

func collectAny(v any, path string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := k
			if path != "" {
				child = path + "." + k
			}
			collectAny(t[k], child, out)
		}
	case []any:
		for i, item := range t {
			collectAny(item, fmt.Sprintf("%s[%d]", path, i), out)
		}
	default:
		out[path] = scalarString(v)
	}
}

// effectivePlatform resolves a playthrough's platform, falling back to the
// game's. A blank entry platform means "same as the game" rather than
// "unknown", so the two must compare equal — otherwise a one-shot game whose
// single entry predates the field would look like a different platform from
// the game it belongs to.
func EffectivePlatform(entryPlatform, gamePlatform string) string {
	if strings.TrimSpace(entryPlatform) != "" {
		return entryPlatform
	}
	return gamePlatform
}
