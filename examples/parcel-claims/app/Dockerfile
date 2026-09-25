# The claims service. Build context: this directory.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/claims .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/claims /usr/local/bin/claims
EXPOSE 8500
ENTRYPOINT ["/usr/local/bin/claims"]
