# Runtime image for axx. Binaries are built by GoReleaser; this only packages them.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/axx /usr/local/bin/axx
ENTRYPOINT ["/usr/local/bin/axx"]
