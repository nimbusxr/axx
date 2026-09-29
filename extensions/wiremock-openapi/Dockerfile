# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0
#
# WireMock standalone with the axx OpenAPI validation extension, GraphQL mocks, model mocks, MCP
# server mocks and A2A agent mocks, and WireMock's gRPC extension (ghcr.io/nimbusxr/axx-wiremock).
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

# WireMock and its gRPC extension, from Maven Central. A download is tried again until its sha256
# is the one pinned here, so a transfer that goes wrong is fetched again, and never used.
FROM --platform=$BUILDPLATFORM eclipse-temurin:21-jdk AS jars
ARG WIREMOCK_VERSION=3.13.2
# sha256 of wiremock-standalone-${WIREMOCK_VERSION}.jar; update together with WIREMOCK_VERSION.
ARG WIREMOCK_SHA256=d097b19bd483c5038479b13a5c71e9faf8f2f5106584f0c120a7770ab0bdb367
# WireMock's own gRPC extension, the build for WireMock standalone; its sha256 changes with it.
ARG GRPC_EXTENSION_VERSION=0.11.0
ARG GRPC_EXTENSION_SHA256=f1e1f822abe07eaff012e059dec5c0aec1e23c8fb3bb3978064a894849e82840
RUN <<'SH'
set -eu
mkdir -p /jars
# fetch <url> <sha256> <file>
fetch() {
  for try in 1 2 3 4 5; do
    if curl -fsSL -o "$3" "$1" && echo "$2  $3" | sha256sum -c --status; then
      return 0
    fi
    echo "$1: try $try failed, or its sha256 is not $2" >&2
    sleep $((try * 2))
  done
  return 1
}
fetch "https://repo1.maven.org/maven2/org/wiremock/wiremock-standalone/${WIREMOCK_VERSION}/wiremock-standalone-${WIREMOCK_VERSION}.jar" \
  "${WIREMOCK_SHA256}" /jars/wiremock-standalone.jar
fetch "https://repo1.maven.org/maven2/org/wiremock/wiremock-grpc-extension-standalone/${GRPC_EXTENSION_VERSION}/wiremock-grpc-extension-standalone-${GRPC_EXTENSION_VERSION}.jar" \
  "${GRPC_EXTENSION_SHA256}" /jars/wiremock-grpc-extension.jar
SH

FROM eclipse-temurin:21-jre-alpine

ARG VERSION=0.1.0-SNAPSHOT

LABEL org.opencontainers.image.title="axx-wiremock" \
      org.opencontainers.image.description="WireMock standalone with the axx OpenAPI validation extension (stubs with metadata.openApiSpecSource are validated against their OpenAPI spec), WireMock's gRPC extension (gRPC services from the descriptor sets in grpc/), GraphQL services and subgraphs mocked field by field from their schema (GRAPHQL_SCHEMA_SOURCE), AI models (the model-request matcher and the model-answer transformer), answered in the format of OpenAI, Anthropic, Gemini, Bedrock or Ollama, MCP servers mocked from their description (MCP_SERVER_SOURCE) with tool calls answered by stubs, and A2A agents mocked from their agent card (A2A_AGENT_CARD_SOURCE) with messages answered by stubs." \
      org.opencontainers.image.source="https://github.com/nimbusxr/axx" \
      org.opencontainers.image.url="https://github.com/nimbusxr/axx/tree/main/extensions/wiremock-openapi" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="NimbusXR" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=jars --chmod=644 /jars/wiremock-standalone.jar /var/wiremock/wiremock-standalone.jar

# The bundled extension lives in lib/ so that mounting jars into /var/wiremock/extensions adds
# extensions instead of hiding this one.
COPY --from=build --chmod=644 /axx-wiremock-openapi.jar /var/wiremock/lib/axx-wiremock-openapi.jar
# On the classpath after wiremock-standalone.jar, so WireMock's classes are the ones loaded,
# and only when the root dir has a grpc folder (entrypoint.sh).
COPY --from=jars --chmod=644 /jars/wiremock-grpc-extension.jar /var/wiremock/grpc/wiremock-grpc-extension.jar
COPY --chmod=755 entrypoint.sh /var/wiremock/entrypoint.sh

# WireMock's root dir is the working directory: mount stubs at /home/wiremock/mappings,
# response bodies at /home/wiremock/__files, and the descriptor sets of gRPC services
# (protoc --descriptor_set_out --include_imports, named *.dsc) at /home/wiremock/grpc: a root
# dir without a grpc folder runs without the gRPC extension.
# WireMock runs as an unprivileged user.
RUN addgroup -S -g 1000 wiremock && adduser -S -D -u 1000 -G wiremock -h /home/wiremock wiremock \
    && mkdir -p /var/wiremock/extensions /home/wiremock/mappings /home/wiremock/__files /home/wiremock/grpc \
    && chmod 0755 /var/wiremock /var/wiremock/lib /var/wiremock/grpc /var/wiremock/extensions \
    && chown -R wiremock:wiremock /home/wiremock
USER wiremock
WORKDIR /home/wiremock

EXPOSE 8080

HEALTHCHECK --interval=1s --timeout=2s --retries=30 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/__admin/health || exit 1

# Arguments are passed to WireMock standalone unchanged (e.g. --verbose, --port). The
# extensions register themselves; no --extensions flag is needed. JAVA_OPTS adds JVM options.
ENTRYPOINT ["/var/wiremock/entrypoint.sh"]
