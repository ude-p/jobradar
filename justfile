set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

build:
    @echo "Building jobradar"
    go build -ldflags="-s -w" -o "$(go env GOPATH)/bin/jobradar" ./cmd

run-dev:
    @set -a && . "$HOME/Library/Application Support/jobradar/.env" && go run ./cmd
