package main

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

// playthroughsFilename is the tool-owned file beside a game's _index.md.
// Hugo reads it as a page resource of the game's bundle, so it travels with
// the game and disappears with it — unlike the achievement archive, which is
// deliberately kept outside content/ so a display decision can't delete it.
const playthroughsFilename = "playthroughs.yaml"

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

	Extra map[string]any `yaml:",inline"`
}

// PlaythroughFields holds the values a form collects for one new entry. It
// stays all-strings because that's what the TUI edits in; SetRating converts.
type PlaythroughFields struct {
	Started  string
	Finished string
	Status   string // playing|finished|dropped|paused
	Platform string // blank means "same as the game's front matter"
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
	return filepath.Join(gameDir, playthroughsFilename)
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
}

// Playthrough is a read-only view of one entry with every field as text,
// which is the shape the forms edit in.
type Playthrough struct {
	Index    int
	Started  string
	Finished string
	Status   string
	Platform string
	Rating   string
	Notes    string
	Sessions []Session
}

func (p Playthrough) HasSessions() bool { return len(p.Sessions) > 0 }

// IsOpen reports whether this playthrough (or, if it has sessions, its most
// recent session) has a blank `finished` field.
func (p Playthrough) IsOpen() bool {
	if p.HasSessions() {
		return p.Sessions[len(p.Sessions)-1].Finished == ""
	}
	return p.Finished == ""
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
			Rating:   e.RatingString(),
			Notes:    e.Notes,
		}
		for j, s := range e.Sessions {
			p.Sessions = append(p.Sessions, Session{Index: j, Started: s.Started, Finished: s.Finished})
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

// SyncStatus keeps a game's sole playthrough entry in step with a
// front-matter status change to finished/dropped/mastered/paused —
// playthroughs.yaml is what actually renders once any playthrough exists,
// and "ongoing" comes from the entry's finished date, not status text, so
// front matter alone can't fix it. Only acts on exactly one playthrough
// (none: nothing to sync; more than one: ambiguous, use "Update a
// playthrough" instead). Never overwrites an already-set finished date, and
// a blank closedOn (as "paused" should always pass) only syncs the status,
// leaving dates untouched. Safe to call unconditionally.
func (f *PlaythroughsFile) SyncStatus(status, closedOn string) bool {
	switch status {
	case "finished", "dropped", "mastered", "paused":
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
func (f *PlaythroughsFile) UpdatePlaythrough(idx int, finished, status, platform, rating, notes string) error {
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
	e.SetRating(rating)
	e.Notes = notes
	return nil
}

// collectYAMLFields flattens a YAML document to "path -> value" for every
// scalar, e.g. "playthroughs[0].sessions[1].finished". Comparing the inventory
// of a file against the inventory of a pending rewrite is how a write proves
// it isn't dropping a field it was never asked to touch.
//
// Only presence is meaningful here, not the values: an unquoted date decodes
// to time.Time while the re-encoded form is a quoted string, so the two sides
// legitimately disagree on representation.
func collectYAMLFields(raw []byte) (map[string]string, error) {
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
func effectivePlatform(entryPlatform, gamePlatform string) string {
	if strings.TrimSpace(entryPlatform) != "" {
		return entryPlatform
	}
	return gamePlatform
}
