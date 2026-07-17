# Reproducible build for local demo / reviewer runs.
# Default: early access website/API backend (persists under SYNTHOS_DATA_DIR, default /data).
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/rpcnode ./cmd/rpcnode \
  && go build -trimpath -ldflags="-s -w" -o /out/devnet ./cmd/devnet \
  && go build -trimpath -ldflags="-s -w" -o /out/synthosd ./cmd/synthosd \
  && go build -trimpath -ldflags="-s -w" -o /out/cloudless-registry ./cmd/cloudless-registry

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /out/rpcnode /out/devnet /out/synthosd /out/cloudless-registry /usr/local/bin/
COPY website/ /website/
# The RPC/validator binaries load genesis and node configuration from /config
# when deployed as containers.
COPY config/*.json /config/
ENV SYNTHOS_DATA_DIR=/data
ENV SYNTHOS_EARLY_ACCESS_WIDGET_PATH=/website/assets/early-access-sale.js
RUN mkdir -p /data
EXPOSE 8080
CMD ["/usr/local/bin/cloudless-registry"]
