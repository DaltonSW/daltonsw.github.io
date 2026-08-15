package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"go.dalton.dog/gamelog/internal/commands"
	"go.dalton.dog/gamelog/internal/server"
)

const envHelp = `Environment (or a .env beside this tool; real env vars take precedence):
  RA_USERNAME, RA_API_KEY    https://retroachievements.org/settings
  STEAM_API_KEY              https://steamcommunity.com/dev/apikey
  STEAM_ID                   SteamID64 or profile vanity name
                             (STEAM_USER_ID works too)
  EXOPHASE_USER              public Exophase profile name, for Ubisoft
                             Connect achievements
  PSN_NPSSO                  npsso session value for PSN trophies, from
                             https://ca.account.sony.com/api/v1/ssocookie
                             after logging into store.playstation.com
  XBLIO_API_KEY              personal key from https://xbl.io, for Xbox

See tools/gamelog/README.md for details.`

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gamelog",
		Short: "Maintain this site's game log in content/games/ and archive/",
		Long: "gamelog — maintain this site's game log in content/games/ and archive/.\n\n" +
			"Run with no subcommand to serve the local web UI (same as \"gamelog serve\").\n\n" +
			envHelp,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return server.Run(server.DefaultPort)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newSuggestCmd(),
		newAchievementsCmd(),
		newProjectCmd(),
		newExophaseCmd(),
		newPSNCmd(),
		newServeCmd(),
	)
	return root
}

func newSuggestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "suggest [slug]",
		Short: "Print suggested playthrough dates from RetroAchievements/Steam",
		Long: "Print suggested playthrough dates from RetroAchievements/Steam for one game.\n" +
			"Read-only: never writes anything. With no slug, prompts to pick a game.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunSuggest(args)
		},
	}
}

func newAchievementsCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "achievements [slug]",
		Short: "Capture unlock history into archive/<provider>/<id>.json",
		Long: "Capture the full unlock history for one game into\n" +
			"archive/<provider>/<id>.json. Re-run to refresh.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) > 0 {
					return fmt.Errorf("cannot combine --all with a slug argument")
				}
				return commands.RunAchievements([]string{"--all"})
			}
			return commands.RunAchievements(args)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "refresh every game that has a provider link, instead of one slug at a time")
	return c
}

func newProjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "project",
		Short: "Regenerate every game's achievement-summary.yaml from the archive on disk",
		Long: "Regenerate every game's achievement-summary.yaml from the archive already\n" +
			"on disk. Makes no API calls.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunProject(nil)
		},
	}
}

func newExophaseCmd() *cobra.Command {
	exophase := &cobra.Command{
		Use:   "exophase",
		Short: "Look up IDs from your Exophase profile",
	}
	exophase.AddCommand(&cobra.Command{
		Use:   "list ubisoft",
		Short: "Print every game on your Exophase profile next to its canonical ID",
		Long: "Print every game on your Exophase profile next to its canonical ID,\n" +
			"for filling in ubisoft_id.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunExophase(append([]string{"list"}, args...))
		},
	})
	return exophase
}

func newPSNCmd() *cobra.Command {
	psn := &cobra.Command{
		Use:   "psn",
		Short: "Look up IDs from your PSN account",
	}
	psn.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Print every PSN trophy title on your account next to its npCommunicationId",
		Long: "Print every PSN trophy title on your account next to its\n" +
			"npCommunicationId, for filling in psn_id.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunPSN([]string{"list"})
		},
	})
	return psn
}

func newServeCmd() *cobra.Command {
	var port int
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve a local web UI over content/games",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return server.Run(port)
		},
	}
	c.Flags().IntVar(&port, "port", server.DefaultPort, "port to listen on (127.0.0.1 only)")
	return c
}

func Exec() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gamelog:", err)
		os.Exit(1)
	}
}
