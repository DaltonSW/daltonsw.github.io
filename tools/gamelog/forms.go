package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
)

const newGameSentinel = "__new__"

func validateDate(required bool) func(string) error {
	return func(s string) error {
		if s == "" {
			if required {
				return fmt.Errorf("a date is required (YYYY-MM-DD)")
			}
			return nil
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("must be a real YYYY-MM-DD date or blank")
		}
		return nil
	}
}

func validateRating(s string) error {
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 10 {
		return fmt.Errorf("must be 1-10 or blank")
	}
	return nil
}

// today is the current date in the site's timezone, so a date typed into a
// form matches the day the rest of the tool would report.
func today() string {
	return time.Now().In(siteLocation).Format("2006-01-02")
}

// SelectGame prompts for an existing game, or the sentinel "create a new
// game" option. Returns slug == newGameSentinel when the latter is chosen.
func SelectGame(games []GameSummary) (string, error) {
	options := []huh.Option[string]{huh.NewOption("+ New game", newGameSentinel)}
	for _, g := range games {
		options = append(options, huh.NewOption(g.Label(), g.Slug))
	}
	var slug string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Which game?").Options(options...).Value(&slug),
		),
	).Run()
	return slug, err
}

// SelectExistingGame prompts for one of an existing set of games, with no
// "+ New game" option — for read-only flows (like `gamelog suggest`) where
// creating a new game makes no sense.
func SelectExistingGame(games []GameSummary) (string, error) {
	if len(games) == 0 {
		return "", fmt.Errorf("no games found to choose from")
	}
	options := make([]huh.Option[string], len(games))
	for i, g := range games {
		options[i] = huh.NewOption(g.SuggestLabel(), g.Slug)
	}
	var slug string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Which game?").Options(options...).Value(&slug),
		),
	).Run()
	return slug, err
}

// SelectCandidates asks which discovered games to create, returning indices
// into candidates. Nothing is preselected — creating content files is opt-in
// per game.
func SelectCandidates(candidates []Candidate) ([]int, error) {
	options := make([]huh.Option[int], len(candidates))
	for i, c := range candidates {
		label := fmt.Sprintf("%s  [%s]", c.Title, c.Provider)
		if c.Finished {
			label += " finished"
		}
		if c.PlaytimeMins > 0 {
			label += " " + formatHours(c.PlaytimeMins)
		}
		options[i] = huh.NewOption(label, i)
	}

	var chosen []int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Create entries for which games?").
				Description("space to toggle, enter to confirm — none selected creates nothing").
				Options(options...).
				Value(&chosen),
		),
	).Run()
	return chosen, err
}

var gameStatuses = []string{"backlog", "playing", "finished", "dropped"}
var playthroughStatuses = []string{"playing", "finished", "dropped", "paused"}

func selectOptions(values []string) []huh.Option[string] {
	opts := make([]huh.Option[string], len(values))
	for i, v := range values {
		opts[i] = huh.NewOption(v, v)
	}
	return opts
}

// NewGameForm prompts for all fields of a brand-new game entry.
func NewGameForm(defaultTitle string) (NewGameFields, error) {
	f := NewGameFields{
		Title:   defaultTitle,
		Status:  "playing",
		Started: today(),
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Title").Value(&f.Title).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("title is required")
				}
				return nil
			}),
			huh.NewInput().Title("Platform").Value(&f.Platform),
			huh.NewInput().Title("RetroAchievements game ID or URL (blank if none)").
				Value(&f.RetroAchievementsID).Validate(validateExternalID),
			huh.NewInput().Title("Steam appid or store URL (blank if none)").
				Value(&f.SteamAppID).Validate(validateExternalID),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(gameStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(validateDate(false)),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(validateDate(false)),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&f.Rating).Validate(validateRating),
			huh.NewText().Title("Overview").Value(&f.Overview),
		),
	).Run()
	if err != nil {
		return f, err
	}
	// Validation above only proves the input is parseable; store the bare ID
	// so front matter never holds a URL.
	if f.RetroAchievementsID, err = parseExternalID(f.RetroAchievementsID); err != nil {
		return f, err
	}
	if f.SteamAppID, err = parseExternalID(f.SteamAppID); err != nil {
		return f, err
	}
	return f, nil
}

// PlaythroughForm prompts for all fields of a new `playthroughs:` entry.
func PlaythroughForm() (PlaythroughFields, error) {
	f := PlaythroughFields{
		Status:  "playing",
		Started: today(),
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Started (YYYY-MM-DD)").Value(&f.Started).Validate(validateDate(true)),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(playthroughStatuses)...).Value(&f.Status),
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&f.Finished).Validate(validateDate(false)),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&f.Rating).Validate(validateRating),
			huh.NewText().Title("Notes").Value(&f.Notes),
		),
	).Run()
	return f, err
}

// SelectPlaythrough prompts to pick one of a game's playthroughs entries.
func SelectPlaythrough(playthroughs []Playthrough) (int, error) {
	options := make([]huh.Option[int], len(playthroughs))
	for i, p := range playthroughs {
		label := fmt.Sprintf("#%d — %s", i+1, p.Status)
		if p.HasSessions() {
			last := p.Sessions[len(p.Sessions)-1]
			label += fmt.Sprintf(" (%d sessions, last %s→%s)", len(p.Sessions), last.Started, orDash(last.Finished))
		} else {
			label += fmt.Sprintf(" (%s→%s)", p.Started, orDash(p.Finished))
		}
		options[i] = huh.NewOption(label, i)
	}
	var idx int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().Title("Which playthrough?").Options(options...).Value(&idx),
		),
	).Run()
	return idx, err
}

func orDash(s string) string {
	if s == "" {
		return "ongoing"
	}
	return s
}

// SessionForm prompts for a new session's date range.
func SessionForm() (started, finished string, err error) {
	started = today()
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Session started (YYYY-MM-DD)").Value(&started).Validate(validateDate(true)),
			huh.NewInput().Title("Session finished (YYYY-MM-DD, blank if still ongoing)").Value(&finished).Validate(validateDate(false)),
		),
	).Run()
	return started, finished, err
}

// UpdateForm prompts to edit an existing playthrough's finished date,
// status, rating, and notes, prefilled with its current values.
func UpdateForm(p Playthrough) (finished, status, rating, notes string, err error) {
	finished = p.Finished
	status = p.Status
	rating = p.Rating
	notes = p.Notes

	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Finished (YYYY-MM-DD, blank if ongoing)").Value(&finished).Validate(validateDate(false)),
			huh.NewSelect[string]().Title("Status").Options(selectOptions(playthroughStatuses)...).Value(&status),
			huh.NewInput().Title("Rating (1-10, blank if none)").Value(&rating).Validate(validateRating),
			huh.NewText().Title("Notes").Value(&notes),
		),
	).Run()
	return finished, status, rating, notes, err
}

// ConfirmWrite shows the lines that will be written and asks for confirmation.
func ConfirmWrite(path string, newLines []string) (bool, error) {
	fmt.Printf("\nAbout to update %s:\n\n", path)
	for _, l := range newLines {
		fmt.Println("  " + l)
	}
	fmt.Println()
	var ok bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title("Write this change?").Value(&ok),
		),
	).Run()
	return ok, err
}
