package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.dalton.dog/gamelog/internal/providers/exophase"
)

// exophaseServices maps the CLI-facing service name to Exophase's own
// environment slug and the front-matter field its canonical ID belongs in —
// "uplay" is Exophase's/Ubisoft's legacy name for Ubisoft Connect, not
// something worth surfacing to a user typing a command. PSN moved off
// Exophase to Sony's own trophy API (see `gamelog psn list`); this map keeps
// its "service name -> slug" shape rather than collapsing to a single
// Ubisoft-only command in case another Exophase-mirrored provider (Nintendo
// is visible on the profile but not yet archived — see KNOWN-ISSUES.md)
// shows up later.
var exophaseServices = map[string]struct {
	env       string
	fmField   string
	titleCase string
}{
	"ubisoft": {exophase.EnvUbisoft, "ubisoft_id", "Ubisoft"},
}

// RunExophase handles `gamelog exophase list ubisoft`. Ubisoft Connect has no
// lookup UI for the canonical_id its front-matter field needs — see
// exophase.go's canonicalIDs — so this just prints every game Exophase
// reports on the configured profile next to the ID to paste.
func RunExophase(args []string) error {
	if len(args) < 1 || args[0] != "list" {
		return fmt.Errorf("usage: gamelog exophase list ubisoft")
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: gamelog exophase list ubisoft")
	}

	svc, ok := exophaseServices[args[1]]
	if !ok {
		return fmt.Errorf("gamelog exophase list: unknown service %q (want \"ubisoft\")", args[1])
	}

	creds := LoadCredentials()
	if !creds.ExophaseConfigured() {
		return fmt.Errorf("EXOPHASE_USER not set")
	}

	client := &exophase.ExophaseClient{User: creds.ExophaseUser}
	games, err := client.Games(context.Background(), svc.env)
	if err != nil {
		return err
	}

	fmt.Print(FormatExophaseList(svc.titleCase, svc.fmField, creds.ExophaseUser, games))
	return nil
}

func FormatExophaseList(titleCase, fmField, user string, games map[string]exophase.Game) string {
	var b strings.Builder

	if len(games) == 0 {
		fmt.Fprintf(&b, "No %s games found on Exophase profile %q.\n", titleCase, user)
		return b.String()
	}

	type row struct {
		id     string
		title  string
		earned int
		total  int
	}
	rows := make([]row, 0, len(games))
	idWidth := 0
	for id, g := range games {
		rows = append(rows, row{id: id, title: g.Meta.Title, earned: g.EarnedAwards, total: g.TotalAwards})
		if len(id) > idWidth {
			idWidth = len(id)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].title) < strings.ToLower(rows[j].title) })

	fmt.Fprintf(&b, "%s games on Exophase profile %q (%d):\n\n", titleCase, user, len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-4s  %s\n", idWidth, r.id, fmt.Sprintf("%d/%d", r.earned, r.total), r.title)
	}
	fmt.Fprintf(&b, "\nPaste the ID next to the game you want into %s in that game's front matter.\n", fmField)
	return b.String()
}
