# syntax=docker/dockerfile:1.7

FROM golang:1.24-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata

# Copy module files first for better build caching.
COPY server/go.mod server/go.sum ./
RUN go mod download

# Copy source and compile static binary.
COPY server/. .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o /out/thefeed ./cmd/thefeed

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/thefeed /app/thefeed

USER nonroot:nonroot
ENTRYPOINT ["/app/thefeed"]
