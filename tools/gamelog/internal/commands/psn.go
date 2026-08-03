package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.dalton.dog/gamelog/internal/providers/psn"
)

// RunPSN handles `gamelog psn list`. There's no lookup UI for a title's
// npCommunicationId anywhere in Sony's own apps, so this prints every trophy
// title on the configured account next to the ID to paste into psn_id — the
// discovery command PSN lost when it moved off Exophase.
func RunPSN(args []string) error {
	if len(args) < 1 || args[0] != "list" {
		return fmt.Errorf("usage: gamelog psn list")
	}

	creds := LoadCredentials()
	if !creds.PSNConfigured() {
		return fmt.Errorf("PSN_NPSSO not set")
	}

	client := &psn.PSNClient{Npsso: creds.PSNNpsso}
	titles, err := client.TrophyTitles(context.Background())
	if err != nil {
		return err
	}

	fmt.Print(FormatPSNList(titles))
	return nil
}

// FormatPSNList renders the same "ID, unlocked/total, title" table
// FormatExophaseList does, sorted by title.
func FormatPSNList(titles map[string]psn.TrophyTitle) string {
	var b strings.Builder

	if len(titles) == 0 {
		fmt.Fprint(&b, "No PSN trophy titles found on this account.\n")
		return b.String()
	}

	type row struct {
		id     string
		title  string
		earned int
		total  int
	}
	rows := make([]row, 0, len(titles))
	idWidth := 0
	for id, t := range titles {
		rows = append(rows, row{
			id:     id,
			title:  t.TrophyTitleName,
			earned: t.EarnedTrophies.Sum(),
			total:  t.DefinedTrophies.Sum(),
		})
		if len(id) > idWidth {
			idWidth = len(id)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].title) < strings.ToLower(rows[j].title) })

	fmt.Fprintf(&b, "PSN trophy titles (%d):\n\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-6s  %s\n", idWidth, r.id, fmt.Sprintf("%d/%d", r.earned, r.total), r.title)
	}
	fmt.Fprint(&b, "\nPaste the ID next to the game you want into psn_id in that game's front matter.\n")
	return b.String()
}
