# Concord server + hub Docker image.
#
# Contains ONLY concord-server and concord-hub -- the client is a TUI app
# and isn't a sensible container workload (see item 12's plan). Both
# binaries are pure Go / CGO-free (server uses modernc.org/sqlite, a
# pure-Go SQLite driver; the hub has no CGO dependency at all), so this
# needs no C toolchain in either build stage.
#
# Neither binary is meant to run its interactive first-run setup wizard
# inside a container -- both already skip that wizard whenever their
# config file already exists (cmd/server/main.go's isFirstRun check,
# cmd/hub/main.go's loadOrSetup), so the documented pattern is: run the
# image once interactively (or run the binary locally) to generate a real
# config file with ToS already accepted, then mount that file into the
# container for all subsequent (detached, non-interactive) runs. See
# docker-compose.yml and the "Docker Deployment" section this ships
# alongside for the concrete one-time-setup steps. There is deliberately
# no env-var/non-interactive bootstrap path here -- building one wasn't
# needed once this pattern was confirmed to already work with zero code
# changes on Concord's side.

FROM golang:1.24-alpine AS build

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ENV CGO_ENABLED=0

RUN go build -ldflags "-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
      -o /out/concord-server ./cmd/server && \
    go build -ldflags "-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
      -o /out/concord-hub ./cmd/hub

# alpine (not scratch) for ca-certificates -- the hub's federation sync and
# a server's Grapevine registration both make outbound HTTP(S) calls to
# other hubs, and a scratch image has no CA trust store for the https:// case.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 concord

COPY --from=build /out/concord-server /out/concord-hub /usr/local/bin/

# Both concord-server.toml and grapevine-hub.toml are read relative to the
# working directory (the hub's path is hardcoded, not flag-configurable --
# see cmd/hub/main.go's `const configPath = "grapevine-hub.toml"`) --
# mount your pre-generated config here. Same for each binary's database
# file and, for the server, its Plugins/ directory. The binaries live on
# PATH (/usr/local/bin), not in this directory, so `docker run <image>
# concord-server` / `concord-hub` works regardless of what's mounted here.
VOLUME ["/app/data"]
WORKDIR /app/data

USER concord

# No ENTRYPOINT/CMD default -- docker-compose.yml (or `docker run ...
# concord-server` / `... concord-hub`) picks which binary to actually run,
# since one image serves both services.
