# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
#
# Mailpit with axx's chaos rules (ghcr.io/nimbusxr/axx-mailpit). A rule refuses the mail of the
# senders or recipients it matches, every time, until it is deleted: tests that each refuse
# their own mail can run side by side, which Mailpit's own chaos triggers (a share of all mail)
# do not allow. chaos-rules.patch adds the rules and their API (/api/v1/chaos/rules); the rest
# is Mailpit, built from its pinned source.

ARG MAILPIT_VERSION=1.31.3

# The binary is built on the build host, for each target platform.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG MAILPIT_VERSION
# sha256 of Mailpit's source archive for MAILPIT_VERSION; update together with it.
ARG MAILPIT_SHA256=51aecda92a1805f5344c30bc079f23562ae659d7f58230dbd1f2b9274414198e
ARG VERSION=dev
ARG TARGETOS TARGETARCH
RUN apk add --no-cache npm patch
ADD --checksum=sha256:${MAILPIT_SHA256} \
    https://github.com/axllent/mailpit/archive/refs/tags/v${MAILPIT_VERSION}.tar.gz /mailpit.tar.gz
WORKDIR /src
RUN tar -xzf /mailpit.tar.gz --strip-components=1
COPY chaos-rules.patch /chaos-rules.patch
RUN patch -p1 < /chaos-rules.patch
RUN --mount=type=cache,target=/root/.npm npm ci && npm run package
RUN --mount=type=cache,target=/root/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go test ./internal/smtpd/... \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
       -ldflags "-s -w -X github.com/axllent/mailpit/config.Version=v${MAILPIT_VERSION}+axx.${VERSION}" -o /mailpit

FROM alpine:3.22

ARG VERSION=dev
ARG MAILPIT_VERSION

LABEL org.opencontainers.image.title="axx-mailpit" \
      org.opencontainers.image.description="Mailpit with axx's chaos rules: refuse the mail of particular senders or recipients, every time, for tests that run side by side." \
      org.opencontainers.image.source="https://github.com/nimbusxr/axx" \
      org.opencontainers.image.url="https://github.com/nimbusxr/axx/tree/main/extensions/mailpit-chaos" \
      org.opencontainers.image.licenses="MIT AND Apache-2.0" \
      org.opencontainers.image.vendor="NimbusXR" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.base.name="axllent/mailpit:v${MAILPIT_VERSION}"

COPY --from=build /mailpit /mailpit
RUN apk add --no-cache tzdata

EXPOSE 1025/tcp 1110/tcp 8025/tcp
HEALTHCHECK --interval=15s --start-period=10s --start-interval=1s CMD ["/mailpit", "readyz"]
ENTRYPOINT ["/mailpit"]
