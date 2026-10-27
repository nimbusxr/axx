# The billing service. Build context: this directory.
FROM golang:1.27-trixie@sha256:c124b9ad3452f46061b5054607d26ea51730dae5372c5e469920063ba2bc6dcc AS build
ARG TARGETARCH
# TensorFlow, which runs the repacks' photo check: its C library's release, pinned, which the
# service links in. TensorFlow publishes its C library for amd64 only.
ARG TENSORFLOW_VERSION=2.18.0
RUN set -eu; \
    test "$TARGETARCH" = amd64 || { echo "TensorFlow's C library is built for amd64 only, not $TARGETARCH" >&2; exit 1; }; \
    curl -fsSL -o /tmp/libtensorflow.tgz "https://storage.googleapis.com/tensorflow/versions/${TENSORFLOW_VERSION}/libtensorflow-cpu-linux-x86_64.tar.gz"; \
    echo "605bfcb370c7e7ec981eabada880f60784e3de018395be95c95c3e5592c3d9a2  /tmp/libtensorflow.tgz" | sha256sum -c -; \
    mkdir -p /opt/tensorflow; \
    tar xzf /tmp/libtensorflow.tgz -C /opt/tensorflow; \
    rm /tmp/libtensorflow.tgz
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# TensorFlow's C library is linked in (-tags tensorflow), from where the image keeps it.
RUN CGO_ENABLED=1 go build -tags tensorflow -trimpath -ldflags="-s -w" -o /out/billing .

FROM gcr.io/distroless/cc-debian13:nonroot@sha256:3ee00291cf386ec52a552e6a858d555debd74776eb4bfa53fa3df2cd6677515e
COPY --from=build /opt/tensorflow/lib /opt/tensorflow/lib
COPY --from=build /out/billing /usr/local/bin/billing
EXPOSE 8600
ENTRYPOINT ["/usr/local/bin/billing"]
