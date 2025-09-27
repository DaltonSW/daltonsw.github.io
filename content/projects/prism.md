---
title: Introducing prism 🔥🪵
showDate: false
date: 2025-09-27
url: /prism
customCSS: gamejams.css
# TODO: Make this not game-jam-specific
---
{{< card >}}

## Welcome! 

Make your unit testing a bit easier on the eyes.

{{</ card >}}

{{< card >}}

## Installation

### Github Releases 🐙

- Go to the `Releases` tab of the repo [here](https://github.com/DaltonSW/prism/releases)
- Download the latest archive for your OS/architecture
- Extract it and place the resulting binary on your `$PATH` and ensure it is executable
```sh
cd ~/Downloads # Assuming you downloaded it here
tar -xvf prism_[whatever].tar.gz # x: Extract; v: Verbose output; f: Specify filename
chmod +x prism # Make file executable
mv prism [somewhere on your $PATH] # Move the file to somewhere on your path for easy execution
```

### Homebrew 🍺 

- Have `brew` installed ([brew.sh](https://brew.sh))
- Run the following:
```sh
brew install --cask daltonsw/tap/prism
```

### Go 🖥️ 

- Have `Go` 
- Have your `Go` install location on your `$PATH`
- Run the following: 
```sh
go install go.dalton.dog/prism@latest
```

{{</ card >}}

{{< card >}}

## Usage

Just run `prism` in your module directory. Anywhere you'd run `go test`, use `prism` instead. That's it!

{{</ card >}}

