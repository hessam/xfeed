# syntax=docker/dockerfile:1.7

FROM golang:1.24-alpine AS builder
WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/. .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o /out/authd ./cmd/authd

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/authd /app/authd
USER nonroot:nonroot
ENTRYPOINT ["/app/authd"]
