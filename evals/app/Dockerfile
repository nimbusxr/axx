# The parcels service on its own, for local development with compose.yaml.
# Build context: evals/ (docker compose -f app/compose.yaml builds it).
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/parcels ./app

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/parcels /usr/local/bin/parcels
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/parcels"]
