package cmd

import (
	"fmt"
	"os"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/interactive"
	"go.dalton.dog/gamelog/internal/server"
)

const usage = `gamelog — maintain this site's game log in content/games/ and archive/.

Usage:
  gamelog                    interactive: pick or create a game, then log a
                             playthrough, log a session, or update one
  gamelog suggest [slug]     print suggested playthrough dates from
                             RetroAchievements/Steam (read-only, never writes)
  gamelog scan [flags]       find games on RetroAchievements/Steam that aren't
                             logged yet, then optionally create entries for them
                               --min-hours N   Steam playtime floor (default 5)
  gamelog review             walk through draft games one at a time: publish,
                             edit, mark dropped, skip, or delete the stub
  gamelog achievements [slug]
                             capture the full unlock history into
                             archive/<provider>/<id>.json (re-run to refresh)
  gamelog achievements --all refresh every game that has a provider link,
                             instead of one slug at a time
  gamelog project             regenerate every game's achievement-summary.yaml
                             from the archive already on disk (no API calls)
  gamelog stale [flags]       walk through "playing" Steam games that have
                             gone quiet, suggesting finished/dropped
                               --days N        quiet threshold (default 30)
  gamelog close [flags]       cap playthroughs left without a closing date at
                             the last day they were played; statuses unchanged
                               --days N        quiet threshold (default 30)
                               --all           include "playing" games too
  gamelog serve [flags]       serve a local web UI over content/games, as an
                             alternative to the interactive terminal flow
                               --port N        port to listen on (default 8080)
  gamelog help               show this message

Environment (or a .env beside this tool; real env vars take precedence):
  RA_USERNAME, RA_API_KEY    https://retroachievements.org/settings
  STEAM_API_KEY              https://steamcommunity.com/dev/apikey
  STEAM_ID                   SteamID64 or profile vanity name
                             (STEAM_USER_ID works too)

See tools/gamelog/README.md for details.`

func isHelpFlag(s string) bool {
	switch s {
	case "help", "-h", "--help":
		return true
	}
	return false
}

func Exec() {
	args := os.Args[1:]

	if len(args) > 0 && isHelpFlag(args[0]) {
		fmt.Println(usage)
		return
	}

	var err error
	switch {
	case len(args) == 0:
		err = interactive.Run()
	case args[0] == "suggest":
		if len(args) > 1 && isHelpFlag(args[1]) {
			fmt.Println(usage)
			return
		}
		err = commands.RunSuggest(args[1:])
	case args[0] == "scan":
		err = commands.RunScan(args[1:])
	case args[0] == "review":
		if len(args) > 1 && isHelpFlag(args[1]) {
			fmt.Println(usage)
			return
		}
		err = commands.RunReview(args[1:])
	case args[0] == "achievements":
		if len(args) > 1 && isHelpFlag(args[1]) {
			fmt.Println(usage)
			return
		}
		err = commands.RunAchievements(args[1:])
	case args[0] == "project":
		err = commands.RunProject(args[1:])
	case args[0] == "stale":
		err = commands.RunStale(args[1:])
	case args[0] == "close":
		err = commands.RunClose(args[1:])
	case args[0] == "serve":
		err = server.Run(args[1:])
	default:
		// Previously any unknown argument silently opened the interactive
		// form, which made a typo look like the tool ignoring you.
		fmt.Fprintf(os.Stderr, "gamelog: unknown command %q\n\n%s\n", args[0], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "gamelog:", err)
		os.Exit(1)
	}
}
