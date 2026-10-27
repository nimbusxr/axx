# The claims service. Build context: this directory.
FROM golang:1.27-trixie@sha256:c124b9ad3452f46061b5054607d26ea51730dae5372c5e469920063ba2bc6dcc AS build
ARG TARGETARCH
# ONNX Runtime, which runs the evidence photo check: its release for the platform, pinned.
ARG ORT_VERSION=1.29.0
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) arch=x64; sum=c3fddc4f139a045b0c4902c57410f0694f1c2fdf9b6939fbe38b1aeae7cd14ba ;; \
      arm64) arch=aarch64; sum=e1799098ebc054b370f6176a450f158720f297818c613e5dc99b92e2ec82346f ;; \
      *) echo "no ONNX Runtime for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/ort.tgz "https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-${arch}-${ORT_VERSION}.tgz"; \
    echo "$sum  /tmp/ort.tgz" | sha256sum -c -; \
    mkdir -p /opt/onnxruntime; \
    tar xzf /tmp/ort.tgz -C /opt/onnxruntime --strip-components=1; \
    cp "/opt/onnxruntime/lib/libonnxruntime.so.${ORT_VERSION}" /opt/onnxruntime/libonnxruntime.so
# llama.cpp, which runs the description triage: built from its release's commit, pinned, as
# static libraries the service links in (the CPU's code for any of the platform's CPUs). Its
# ggml is whisper.cpp's too: the release's ggml headers are whisper.cpp's release's below.
ARG LLAMA_VERSION=b11435
ARG LLAMA_COMMIT=43fe9c64281ef735046adc025e9e7559a1f659a5
RUN set -eu; \
    apt-get update; apt-get install -y --no-install-recommends cmake; rm -rf /var/lib/apt/lists/*; \
    git clone --quiet --depth 1 --branch "$LLAMA_VERSION" https://github.com/ggml-org/llama.cpp /tmp/llama.cpp; \
    test "$(git -C /tmp/llama.cpp rev-parse HEAD)" = "$LLAMA_COMMIT"; \
    cmake -S /tmp/llama.cpp -B /tmp/llama.cpp/build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
      -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF -DLLAMA_BUILD_COMMON=OFF -DLLAMA_BUILD_TESTS=OFF \
      -DLLAMA_BUILD_TOOLS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=OFF -DLLAMA_BUILD_APP=OFF > /dev/null; \
    cmake --build /tmp/llama.cpp/build -j "$(nproc)" > /dev/null; \
    cmake --install /tmp/llama.cpp/build --prefix /opt/llama > /dev/null; \
    rm -rf /tmp/llama.cpp
# whisper.cpp, which runs the voice notes' speech recognizer: built from its release's commit,
# pinned, as a static library the service links in, on llama.cpp's ggml (two copies of ggml
# cannot be linked into one program).
ARG WHISPER_VERSION=v1.9.5
ARG WHISPER_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
RUN set -eu; \
    git clone --quiet --depth 1 --branch "$WHISPER_VERSION" https://github.com/ggml-org/whisper.cpp /tmp/whisper.cpp; \
    test "$(git -C /tmp/whisper.cpp rev-parse HEAD)" = "$WHISPER_COMMIT"; \
    cmake -S /tmp/whisper.cpp -B /tmp/whisper.cpp/build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
      -DCMAKE_PREFIX_PATH=/opt/llama -DWHISPER_USE_SYSTEM_GGML=ON \
      -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF -DWHISPER_BUILD_SERVER=OFF > /dev/null; \
    cmake --build /tmp/whisper.cpp/build -j "$(nproc)" > /dev/null; \
    cmake --install /tmp/whisper.cpp/build --prefix /opt/whisper > /dev/null; \
    rm -rf /tmp/whisper.cpp
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The ONNX Runtime binding is cgo, its library loaded when the service starts; llama.cpp and
# whisper.cpp are linked in (-tags llama,whisper).
RUN CGO_ENABLED=1 go build -tags llama,whisper -trimpath -ldflags="-s -w" -o /out/claims .

FROM gcr.io/distroless/cc-debian13:nonroot@sha256:3ee00291cf386ec52a552e6a858d555debd74776eb4bfa53fa3df2cd6677515e
COPY --from=build /opt/onnxruntime/libonnxruntime.so /usr/local/lib/libonnxruntime.so
COPY --from=build /out/claims /usr/local/bin/claims
EXPOSE 8500
ENTRYPOINT ["/usr/local/bin/claims"]
