# The claims service. Build context: this directory.
FROM golang:1.27-trixie@sha256:c124b9ad3452f46061b5054607d26ea51730dae5372c5e469920063ba2bc6dcc AS build
ARG TARGETARCH
# ONNX Runtime, which runs the evidence photo check: its release for the platform, pinned.
# When its GitHub repository answers 404 (it went offline on 2026-11-17), the library comes
# from the same release's jar on Maven Central, pinned too. The arm64 library there is the very file
# of GitHub's release (the same SHA-256 as in an image built from it); the amd64 one could not be
# compared while GitHub is away. A download that the network or the server (a 5xx) fails is
# tried again, three times (--retry). GitHub's 404 is an answer: it is asked for once.
ARG ORT_VERSION=1.29.0
ARG ORT_JAR_SHA256=5933bbc0c6c4d89afc04bf7c11011de23a947509c1352e5bf5936e62a84e4d10
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) arch=x64; sum=c3fddc4f139a045b0c4902c57410f0694f1c2fdf9b6939fbe38b1aeae7cd14ba; \
             lib=5715f06d8992ca8eeeddcce43df3a7d38f97d537052126f558e912cb312460ca ;; \
      arm64) arch=aarch64; sum=e1799098ebc054b370f6176a450f158720f297818c613e5dc99b92e2ec82346f; \
             lib=a27d21126db312aa8f02f3d5eaebe466e991f51f469882e6d0407d5a8b64afda ;; \
      *) echo "no ONNX Runtime for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    mkdir -p /opt/onnxruntime; \
    release="https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-${arch}-${ORT_VERSION}.tgz"; \
    answer=$(curl -sSL --retry 3 --retry-all-errors -o /tmp/ort.tgz -w '%{http_code}' "$release"); \
    case "$answer" in \
      200) \
        echo "$sum  /tmp/ort.tgz" | sha256sum -c -; \
        tar xzf /tmp/ort.tgz -C /opt/onnxruntime --strip-components=1; \
        cp "/opt/onnxruntime/lib/libonnxruntime.so.${ORT_VERSION}" /opt/onnxruntime/libonnxruntime.so ;; \
      404) \
        echo "ONNX Runtime ${ORT_VERSION} is not on GitHub (404): taking it from Maven Central" >&2; \
        curl -fsSL --retry 3 --retry-all-errors -o /tmp/ort.jar "https://repo1.maven.org/maven2/com/microsoft/onnxruntime/onnxruntime/${ORT_VERSION}/onnxruntime-${ORT_VERSION}.jar"; \
        echo "$ORT_JAR_SHA256  /tmp/ort.jar" | sha256sum -c -; \
        apt-get update; apt-get install -y --no-install-recommends unzip; rm -rf /var/lib/apt/lists/*; \
        unzip -p /tmp/ort.jar "ai/onnxruntime/native/linux-${arch}/libonnxruntime.so" > /opt/onnxruntime/libonnxruntime.so; \
        echo "$lib  /opt/onnxruntime/libonnxruntime.so" | sha256sum -c - ;; \
      *) echo "ONNX Runtime ${ORT_VERSION}: GitHub answered $answer" >&2; exit 1 ;; \
    esac; \
    rm -f /tmp/ort.tgz /tmp/ort.jar
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
