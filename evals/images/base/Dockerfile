# syntax=docker/dockerfile:1.7
#
# Images for axx's agent evals. Build context: the repository root. Built by
# `go run ./cmd/evals images` (from evals/):
#
#   --target base      axx-evals-base: the agent's environment. axx built from
#                      this checkout with every pack compiled in (so projects
#                      never wait for axx to prepare their packs), the parcels
#                      service and the common starting project
#                      (/opt/evals/workspace).
#   --target verifier  axx-evals-verifier: base plus the verifier. Only the
#                      separate verifier container uses it, so the agent never
#                      sees the checks or the list of deliberate bugs.

ARG GO_IMAGE=golang:1.27
ARG NODE_IMAGE=node:22-bookworm-slim
ARG UV_IMAGE=ghcr.io/astral-sh/uv:0.9.11

FROM ${GO_IMAGE} AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY evals/go.mod evals/go.sum ./evals/
RUN --mount=type=cache,target=/go/pkg/mod cd evals && go mod download
COPY . .
ARG AXX_VERSION=0.0.0-evals
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X github.com/nimbusxr/axx/internal/version.Version=${AXX_VERSION}" \
      -o /out/axx ./internal/tools/axxall
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    cd evals \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/parcels ./app \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verifier/evals-verify ./cmd/evals-verify

FROM ${UV_IMAGE} AS uv

FROM ${NODE_IMAGE} AS base
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash ca-certificates curl git jq less procps python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*
COPY --from=uv /uv /uvx /usr/local/bin/
COPY --from=go /out/axx /out/parcels /usr/local/bin/
RUN git config --system user.name "Parcels team" \
    && git config --system user.email "parcels-team@example.com" \
    && git config --system init.defaultBranch main \
    && git config --system --add safe.directory '*' \
    && useradd --create-home --uid 1001 parcels \
    && useradd --create-home --uid 1002 tester
COPY evals/workspace /opt/evals/workspace
COPY evals/images/base/bin/ /usr/local/bin/
WORKDIR /app

FROM base AS verifier
COPY --from=go /out/verifier/ /opt/evals/bin/
