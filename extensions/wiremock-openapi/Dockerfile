# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
#
# WireMock standalone with the axx OpenAPI validation extension (ghcr.io/nimbusxr/axx-wiremock).
#
# The extension needs Java 21 (openapi-request-validator 3.x), which the official
# wiremock/wiremock image's JRE 17 cannot load, so this image ships its own runtime. It also
# pins one WireMock version in one place: the one the extension compiles and tests against
# (keep WIREMOCK_VERSION in sync with build.gradle.kts).

# The jar is platform-independent: build it once on the build host, even for multi-arch images.
FROM --platform=$BUILDPLATFORM eclipse-temurin:21-jdk AS build
ARG VERSION=0.1.0-SNAPSHOT
WORKDIR /src
COPY gradlew settings.gradle.kts build.gradle.kts gradle.properties ./
COPY gradle ./gradle
COPY src/main ./src/main
RUN --mount=type=cache,target=/root/.gradle \
    ./gradlew --no-daemon --console=plain shadowJar -Pversion="${VERSION}" \
    && cp build/libs/axx-wiremock-openapi-*-all.jar /axx-wiremock-openapi.jar

FROM eclipse-temurin:21-jre-alpine

ARG VERSION=0.1.0-SNAPSHOT
ARG WIREMOCK_VERSION=3.13.2
# sha256 of wiremock-standalone-${WIREMOCK_VERSION}.jar; update together with WIREMOCK_VERSION.
ARG WIREMOCK_SHA256=d097b19bd483c5038479b13a5c71e9faf8f2f5106584f0c120a7770ab0bdb367

LABEL org.opencontainers.image.title="axx-wiremock" \
      org.opencontainers.image.description="WireMock standalone with the axx OpenAPI validation extension: stubs with metadata.openApiSpecSource are validated against their OpenAPI spec." \
      org.opencontainers.image.source="https://github.com/nimbusxr/axx" \
      org.opencontainers.image.url="https://github.com/nimbusxr/axx/tree/main/extensions/wiremock-openapi" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="NimbusXR" \
      org.opencontainers.image.version="${VERSION}"

ADD --checksum=sha256:${WIREMOCK_SHA256} --chmod=644 \
    https://repo1.maven.org/maven2/org/wiremock/wiremock-standalone/${WIREMOCK_VERSION}/wiremock-standalone-${WIREMOCK_VERSION}.jar \
    /var/wiremock/wiremock-standalone.jar

# The bundled extension lives in lib/ so that mounting jars into /var/wiremock/extensions adds
# extensions instead of hiding this one.
COPY --from=build --chmod=644 /axx-wiremock-openapi.jar /var/wiremock/lib/axx-wiremock-openapi.jar

# WireMock's root dir is the working directory: mount stubs at /home/wiremock/mappings and
# response bodies at /home/wiremock/__files. WireMock runs as an unprivileged user.
RUN addgroup -S -g 1000 wiremock && adduser -S -D -u 1000 -G wiremock -h /home/wiremock wiremock \
    && mkdir -p /var/wiremock/extensions /home/wiremock/mappings /home/wiremock/__files \
    && chmod 0755 /var/wiremock /var/wiremock/lib /var/wiremock/extensions \
    && chown -R wiremock:wiremock /home/wiremock
USER wiremock
WORKDIR /home/wiremock

EXPOSE 8080

HEALTHCHECK --interval=1s --timeout=2s --retries=30 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/__admin/health || exit 1

# Arguments are passed to WireMock standalone unchanged (e.g. --verbose, --port). The
# extension registers itself; no --extensions flag is needed. JAVA_OPTS adds JVM options.
ENTRYPOINT ["sh", "-c", "exec java $JAVA_OPTS -cp '/var/wiremock/wiremock-standalone.jar:/var/wiremock/lib/*:/var/wiremock/extensions/*' wiremock.Run \"$@\"", "--"]
