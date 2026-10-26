# The support desk's service: Python 3.12, with its dependencies as uv.lock pins them.
FROM python:3.12-slim-trixie@sha256:a0f57064cbdcd6285fa45f51237ab0b35ea8ebaed06a1f5973e89e19dfb86704 AS build
# llama-cpp-python builds llama.cpp, which runs the reranker in the service, from its
# sources: for any of the platform's CPUs, not only the builder's.
RUN apt-get update && apt-get install -y --no-install-recommends build-essential cmake && rm -rf /var/lib/apt/lists/*
COPY --from=ghcr.io/astral-sh/uv:0.9.30@sha256:538e0b39736e7feae937a65983e49d2ab75e1559d35041f9878b7b7e51de91e4 /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never CMAKE_ARGS="-DGGML_NATIVE=OFF"
WORKDIR /app
COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-install-project

# Piper's command-line program (MIT), which speaks the call-backs' confirmations: its release
# for the platform's CPU, pinned by its SHA-256.
FROM python:3.12-slim-trixie@sha256:a0f57064cbdcd6285fa45f51237ab0b35ea8ebaed06a1f5973e89e19dfb86704 AS piper
ARG TARGETARCH
RUN python - <<'EOF'
import hashlib, os, tarfile, urllib.request
arch, sha256 = {
    "amd64": ("x86_64", "a50cb45f355b7af1f6d758c1b360717877ba0a398cc8cbe6d2a7a3a26e225992"),
    "arm64": ("aarch64", "fea0fd2d87c54dbc7078d0f878289f404bd4d6eea6e7444a77835d1537ab88eb"),
}[os.environ["TARGETARCH"]]
url = f"https://github.com/rhasspy/piper/releases/download/2023.11.14-2/piper_linux_{arch}.tar.gz"
data = urllib.request.urlopen(url).read()
if hashlib.sha256(data).hexdigest() != sha256:
    raise SystemExit(f"{url} is not the release pinned")
with open("/tmp/piper.tar.gz", "wb") as f:
    f.write(data)
with tarfile.open("/tmp/piper.tar.gz") as tar:
    tar.extractall("/opt", filter="data")
EOF

# parakeet.cpp's command-line program (MIT), which runs NVIDIA's Parakeet for the transcriber's
# parakeet.cpp engine: its release for the platform's CPU, pinned by its SHA-256.
FROM python:3.12-slim-trixie@sha256:a0f57064cbdcd6285fa45f51237ab0b35ea8ebaed06a1f5973e89e19dfb86704 AS parakeet
ARG TARGETARCH
RUN python - <<'EOF'
import hashlib, os, tarfile, urllib.request
arch, sha256 = {
    "amd64": ("x64", "cce60d122ab72e1068cd0d164e54a21655a0b83f1b9c21befc20124f5a972c10"),
    "arm64": ("arm64", "85b6dafce8a984d0971d94865da5e2e50e111604fb6e7030f5b599dc2616c8db"),
}[os.environ["TARGETARCH"]]
url = f"https://github.com/mudler/parakeet.cpp/releases/download/v0.6.1/parakeet-v0.6.1-bin-linux-cpu-{arch}.tar.gz"
data = urllib.request.urlopen(url).read()
if hashlib.sha256(data).hexdigest() != sha256:
    raise SystemExit(f"{url} is not the release pinned")
with open("/tmp/parakeet.tar.gz", "wb") as f:
    f.write(data)
with tarfile.open("/tmp/parakeet.tar.gz") as tar:
    tar.extractall("/tmp", filter="data")
os.makedirs("/opt/parakeet")
os.replace(f"/tmp/parakeet-v0.6.1-bin-linux-cpu-{arch}/parakeet-cli", "/opt/parakeet/parakeet-cli")
EOF

FROM python:3.12-slim-trixie@sha256:a0f57064cbdcd6285fa45f51237ab0b35ea8ebaed06a1f5973e89e19dfb86704
# ffmpeg: OpenAI's whisper and transformers read recordings with it. tesseract-ocr: the
# system's Tesseract, of its English model, reads labels' reference lines. libegl1 and
# libgles2: MediaPipe's library loads them, whatever its task.
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg tesseract-ocr libegl1 libgles2 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /app/.venv /app/.venv
COPY --from=piper /opt/piper /opt/piper
COPY --from=parakeet /opt/parakeet /opt/parakeet
COPY support ./support
RUN useradd --system --uid 10001 support
USER support
# The models are the folders mounted at /models: nothing is downloaded.
ENV PATH=/app/.venv/bin:/opt/piper:/opt/parakeet:$PATH HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1
# Keras runs on PyTorch, which the service has for its other models. Matplotlib, which
# MediaPipe imports, and fontconfig keep their caches where the service's user can write.
ENV KERAS_BACKEND=torch MPLCONFIGDIR=/tmp/matplotlib XDG_CACHE_HOME=/tmp/cache
# MNN's library for x86-64 asks for an executable stack, which glibc 2.41 (trixie's) gives a
# library it loads only when this says it may: without it, the photo check's mnn engine cannot
# import MNN there.
ENV GLIBC_TUNABLES=glibc.rtld.execstack=2
EXPOSE 8800
CMD ["uvicorn", "support.app:app", "--host", "0.0.0.0", "--port", "8800"]
