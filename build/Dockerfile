# Runtime image for axx. Binaries are built by GoReleaser; this only packages them.
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/axx /usr/local/bin/axx
ENTRYPOINT ["/usr/local/bin/axx"]
