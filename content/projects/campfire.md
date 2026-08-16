---
title: campfire
tagline: "A cozy companion for tailing and filtering log files."
dev_status: stable
category: main
date: 2025-08-07
repo: https://github.com/DaltonSW/campfire
links:
  - { label: "Go Package", url: "https://pkg.go.dev/go.dalton.dog/campfire" }
weight: 20
draft: false
---
{{< card >}}

## Welcome! 

Make your log files helpful and beautiful instead of scary and cumbersome.

- Monitor files as they update in real time
- Filter your files by log type or keyword, hiding things you don't care about
- Continously monitor files by name, whether they exist or not
- All of the above at once!

{{</ card >}}

{{< card >}}

## Installation

### Github Releases 🐙

- Go to the `Releases` tab of the repo [here](https://github.com/DaltonSW/campfire/releases)
- Download the latest archive for your OS/architecture
- Extract it and place the resulting binary on your `$PATH` and ensure it is executable
```sh
cd ~/Downloads # Assuming you downloaded it here
tar -xvf campfire_[whatever].tar.gz # x: Extract; v: Verbose output; f: Specify filename
chmod +x campfire # Make file executable
mv campfire [somewhere on your $PATH] # Move the file to somewhere on your path for easy execution
```

### Homebrew 🍺 

- Have `brew` installed ([brew.sh](https://brew.sh))
- Run the following:
```sh
brew install --cask daltonsw/tap/campfire
```

### Go 🖥️ 

- Have `Go` 
- Have your `Go` install location on your `$PATH`
- Run the following: 
```sh
go install go.dalton.dog/campfire@latest
```

{{</ card >}}

{{< card >}}

## Usage

- Just run `campfire [file]` with whatever file you want to monitor. That's it!

{{</ card >}}

