# me

A small Cobra-based Go CLI under `go/cli`.

## Run

```sh
go run ./cmd/me version
```

## Test

```sh
go test ./...
```

## Build

```sh
go build -o ./bin/me ./cmd/me
```

## Release Metadata

```sh
go build \
  -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o ./bin/me ./cmd/me
```
