---
title: Introducing aocli
showDate: false
url: /aocli
draft: true
---

`aocli` is a command-line designed to interface with Advent of Code without leaving your terminal.
Written in `Go` to ensure speediness, and using `bubbletea` and `lipgloss` for pretiness, aocli strives to enhance your Advent of Code experience.

With it, you can:
- get your puzzle input
- view the puzzle for a day
- submit your guess
- check out leaderboards

## Installation

You can install `aocli` in one of the following ways, listed in recommended order. `aocli` has a self-updater built in, so you can use that to update it going forward.

### GitHub Releases

Check out the latest GitHub release, found [here](). Download the version for your OS.

### Homebrew

*Note: Can't be used on Windows*

Run the following commands:
```sh
brew tap daltonsw-packages
brew install aocli
```

### Using Go

*Note: Might not be the most up-to-date version, since this will match to the module version*

Ensure you have `Go` installed. Then run the following: 

`go install go.dalton.dog/aocli@latest`

Ensure you have `$GOPATH` on your `$PATH`.

## After Installation

Check out the repository [here](). It has required setup instructions and a list of available commands with examples.

## Shoutouts

- [Eric Wastl](http://was.tl): Creator of [Advent of Code](https://adventofcode.com). Thanks for providing years of entertainment and fun puzzles :)
- [CharmBracelet](https://charm.sh): Creators of [BubbleTea](https://github.com/charmbracelet/bubbletea) and [LipGloss](https://github.com/charmbraclet/lipgloss)
- My friend, David, who guinea-pig'd the program along the way
