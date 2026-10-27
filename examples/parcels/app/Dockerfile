# The parcels service. Build context: this directory.
FROM golang:1.27@sha256:9048d5e7751706200eb1719c7dae0436950d362f29a1da91fa05e5c63867c60f AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/parcels .

# What the portal's pages run their models with: transformers.js, and ONNX
# Runtime's WebAssembly; TensorFlow.js (its ES module, and the source map it
# names); WebLLM (and its source map, which it names index.js.map), as
# portal-vendor's lockfile pins them. And the model
# library WebLLM runs the return reasons' model with (SmolLM2-135M's, of
# float32s), of WebLLM's prebuilt libraries' commit.
FROM node:24-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53 AS portal-vendor
WORKDIR /vendor
COPY portal-vendor/package.json portal-vendor/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund \
 && mkdir /out \
 && cp node_modules/@huggingface/transformers/dist/transformers.min.js /out/ \
 && cp node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded*.mjs node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded*.wasm /out/ \
 && cp node_modules/@tensorflow/tfjs/dist/tf.fesm.min.js node_modules/@tensorflow/tfjs/dist/tf.fesm.min.js.map /out/ \
 && cp node_modules/@mlc-ai/web-llm/lib/index.js /out/web-llm.js \
 && cp node_modules/@mlc-ai/web-llm/lib/index.js.map /out/index.js.map
ADD --chmod=644 --checksum=sha256:e73dc9f4b165e4488b532dee6df1f07b64fdd7e22438ae953387526ca5f13e3b \
  https://raw.githubusercontent.com/mlc-ai/binary-mlc-llm-libs/025bcaf3780fa8254f5e5efd3bfea0a5397248f4/web-llm-models/v0_2_84/base/SmolLM2-135M-Instruct-q4f32_1_cs1k-webgpu.wasm \
  /out/

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/parcels /usr/local/bin/parcels
COPY --from=portal-vendor /out /usr/share/parcels/portal-vendor
ENV PARCELS_PORTAL_VENDOR=/usr/share/parcels/portal-vendor
EXPOSE 8400
ENTRYPOINT ["/usr/local/bin/parcels"]
