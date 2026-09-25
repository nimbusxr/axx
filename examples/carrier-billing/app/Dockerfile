# The billing service. Build context: this directory.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/billing .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/billing /usr/local/bin/billing
EXPOSE 8600
ENTRYPOINT ["/usr/local/bin/billing"]
