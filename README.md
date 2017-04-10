# pare
CLI client for Condenser

## Installing
If you don't have a working Go installation, [install one first](https://golang.org/doc/install).

To install, run `go get github.com/emberwalker/pare` and ensure that `$GOPATH/bin` is in your `PATH`.

## Configuration
When starting, `pare` loads configuration from the file `$HOME/.pare.json`, which can be overriden by
the command line flags `--server` and `--apikey`. An example `.pare.json` is provided in this repo.
