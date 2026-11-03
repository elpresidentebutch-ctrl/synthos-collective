# Reproducible build for local demo / reviewer runs.
# Default: early access website/API backend (persists under SYNTHOS_DATA_DIR, default /data).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY cometapp/ ./cometapp/
ENV CGO_ENABLED=0
RUN mkdir -p /out/downloads \
  && go build -trimpath -ldflags="-s -w" -o /out/rpcnode ./cmd/rpcnode \
  && go build -trimpath -ldflags="-s -w" -o /out/devnet ./cmd/devnet \
  && go build -trimpath -ldflags="-s -w" -o /out/synthosd ./cmd/synthosd \
  && go build -trimpath -ldflags="-s -w" -o /out/cloudless-registry ./cmd/cloudless-registry \
  && (cd cometapp && go build -trimpath -ldflags="-s -w" -o /out/synthos-comet .) \
  && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/downloads/silentnode.exe ./cmd/silentnode \
  && GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/downloads/synthos-node-linux-amd64 ./cmd/silentnode \
  && GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /out/downloads/synthos-node-linux-arm64 ./cmd/silentnode \
  && GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /out/downloads/synthos-node-darwin-arm64 ./cmd/silentnode \
  && GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/downloads/synthos-node-darwin-amd64 ./cmd/silentnode

FROM alpine:3.19
RUN apk add --no-cache ca-certificates su-exec \
  && addgroup -g 10001 -S synthos \
  && adduser -u 10001 -S synthos -G synthos
COPY --from=build /out/rpcnode /out/devnet /out/synthosd /out/synthos-comet /out/cloudless-registry /usr/local/bin/
COPY --from=build /out/downloads/ /downloads/
COPY website/ /website/
RUN cp -f /website/downloads/synthos-node.apk /downloads/synthos-node.apk 2>/dev/null || true
# The RPC/validator binaries load genesis and node configuration from /config
# when deployed as containers.
COPY config/*.json /config/
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh \
  && mkdir -p /data /downloads \
  && chown -R synthos:synthos /data /downloads /website /config

ENV SYNTHOS_DATA_DIR=/data
ENV SYNTHOS_EARLY_ACCESS_WIDGET_PATH=/website/assets/early-access-sale.js
ENV GOMEMLIMIT=256MiB
ENV GOGC=30
ENV SYNTHOS_MAX_HOT_BLOCKS=250

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/cloudless-registry"]
