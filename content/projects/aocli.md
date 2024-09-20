---
title: Introducing aocli
showDate: false
date: 2024-09-12
url: /aocli
customCSS: gamejams.css
# TODO: Make this not game-jam-specific
---
{{< card >}}

## Welcome!

`aocli` is a command-line designed to interface with Advent of Code without leaving your terminal.
Written in `Go` to ensure speediness, and using `bubbletea` and `lipgloss` for pretiness, aocli strives to enhance your Advent of Code experience.

With it, you can:
- get your puzzle input
- view the puzzle for a day
- submit your guess
- check out leaderboards
- look at your user information

!["aocli Landing Page"](/images/aocliLandingPage.png)

{{</ card >}}

{{< card >}}
## Installation

You can install `aocli` in one of the following ways, listed in recommended order. `aocli` has a self-updater built in, so you can use that to update it going forward.

### GitHub Releases

Check out the latest GitHub release, found [here](https://github.com/DaltonSW/aocgo/releases/latest). Download the version for your OS.

### Homebrew

*Note: Can't be used on Windows*

Run the following commands:
```sh
brew tap daltonsw-packages
brew install aocli
```

### Using Go

*Note: Might not be the most up-to-date version, since this will match to the module version*

Ensure you have `Go` [installed](https://go.dev/doc/install). Then, run the following: 

`go install go.dalton.dog/aocli@latest`

Ensure you have `$GOPATH` on your `$PATH`.

{{</ card >}}

{{< card >}}

## Required Setup

1. Go to [Advent of Code](https://adventofcode.com) and login with your preferred method
2. Access Dev Tools (either F12 or Right-Click -> Inspect)
3. Click the `Storage` tab at the top of the section
4. Locate the `session` token. Double click the `Value` and copy it
5. Place the session token in one of the following places:
    - `~/.config/aocgo/session.token`
    - `AOC_SESSION_TOKEN` environment variable*
6. Run `aocli health` to ensure the program can find the token properly

*\*If you choose the environment variable method, ensure you include a line to set it in your shell's startup script so it gets set every launch.*

{{</ card >}}

{{< card >}}

## After Installation

To view the available commands, check out the command list [here](https://github.com/DaltonSW/aocgo/tree/main/cmd/aocli) or run `aocli help` once installed.

Check out the repository [here](https://github.com/DaltonSW/aocgo/tree/main/cmd/aocli). It has required setup instructions and a list of available commands with examples.

{{</ card >}}

{{< card >}}

## Shoutouts

- [Eric Wastl](http://was.tl): Creator of [Advent of Code](https://adventofcode.com). Thanks for providing years of entertainment and fun puzzles :)
- [CharmBracelet](https://charm.sh): Creators of [BubbleTea](https://github.com/charmbracelet/bubbletea) and [LipGloss](https://github.com/charmbraclet/lipgloss)
- My friend, David, who guinea-pig'd the program along the way

{{</ card >}}
