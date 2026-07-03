# Reproducible build for local demo / reviewer runs.
# Default: HTTP RPC node (persists under SYNTHOS_DATA_DIR, default /data).
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
ENV SYNTHOS_DATA_DIR=/data
RUN mkdir -p /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/rpcnode"]
