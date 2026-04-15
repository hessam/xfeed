FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
RUN git clone https://github.com/net2share/vaydns.git .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /vaydns-server ./vaydns-server

FROM alpine
COPY --from=build /vaydns-server /usr/local/bin/
# Generate keys if they don't exist and run
ENTRYPOINT ["/bin/sh", "-c", "if [ ! -f /keys/priv.key ] || [ ! -s /keys/priv.key ]; then /usr/local/bin/vaydns-server -gen-key > /keys/keys.txt && grep 'privkey' /keys/keys.txt | awk '{print $2}' > /keys/priv.key && grep 'pubkey' /keys/keys.txt | awk '{print $2}' > /keys/pub.key; echo 'VayDNS Keys Generated:'; cat /keys/keys.txt; fi; exec /usr/local/bin/vaydns-server -udp=:53 -domain=${THEFEED_DOMAIN} -upstream=thefeed:5300 -privkey=$(cat /keys/priv.key)"]
