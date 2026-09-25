# The parcels service. Build context: this directory.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/parcels .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/parcels /usr/local/bin/parcels
EXPOSE 8400
ENTRYPOINT ["/usr/local/bin/parcels"]
