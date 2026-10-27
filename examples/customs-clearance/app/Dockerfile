# The customs service. Build context: this directory.
FROM golang:1.27-trixie@sha256:c124b9ad3452f46061b5054607d26ea51730dae5372c5e469920063ba2bc6dcc AS build
ARG TARGETARCH
# OpenVINO, which runs the inspections' contents check: its release for the platform, pinned,
# whose C library the service links in.
ARG OPENVINO_VERSION=2026.4.1
ARG OPENVINO_BUILD=22982.07f9c262b05
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) arch=x86_64; lib=intel64; sum=e2541afe65a81d5aabe981f9a86ad2c7efe557922b71e5649b6aafffec7c982b ;; \
      arm64) arch=arm64; lib=aarch64; sum=09712347aa8c14f1c7f9cbf05150ccc0057c67b6c80762b330b4be23cadc8cf1 ;; \
      *) echo "no OpenVINO for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    name="openvino_toolkit_ubuntu22_${OPENVINO_VERSION}.${OPENVINO_BUILD}_${arch}"; \
    curl -fsSL -o /tmp/openvino.tgz "https://storage.openvinotoolkit.org/repositories/openvino/packages/${OPENVINO_VERSION}/linux/${name}.tgz"; \
    echo "$sum  /tmp/openvino.tgz" | sha256sum -c -; \
    tar xzf /tmp/openvino.tgz -C /tmp; \
    mkdir -p /opt/openvino/lib; \
    cp -a "/tmp/${name}/runtime/include" /opt/openvino/include; \
    cp -a "/tmp/${name}/runtime/lib/${lib}/." "/tmp/${name}/runtime/3rdparty/tbb/lib/." /opt/openvino/lib/; \
    rm -rf /opt/openvino/lib/cmake /opt/openvino/lib/pkgconfig /tmp/openvino.tgz "/tmp/${name}"
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# OpenVINO's C library is linked in (-tags openvino), from where the image keeps it.
RUN CGO_ENABLED=1 go build -tags openvino -trimpath -ldflags="-s -w" -o /out/customs .

FROM gcr.io/distroless/cc-debian13:nonroot@sha256:3ee00291cf386ec52a552e6a858d555debd74776eb4bfa53fa3df2cd6677515e
COPY --from=build /opt/openvino/lib /opt/openvino/lib
COPY --from=build /out/customs /usr/local/bin/customs
EXPOSE 8700
ENTRYPOINT ["/usr/local/bin/customs"]
