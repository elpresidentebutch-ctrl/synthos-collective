# SYNTHOS Global Node Dockerfile for Hugging Face Spaces
FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
RUN git clone https://github.com/elpresidentebutch-ctrl/synthos-collective.git .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/synthosd ./cmd/synthosd

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /out/synthosd /usr/local/bin/
COPY --from=build /src/config/*.json /config/

ENV SYNTHOS_CONFIG=/config/global-node.json
ENV SYNTHOS_DATA_DIR=/data
ENV SYNTHOS_NODE_ID=synthos-hf-space-1
ENV SYNTHOS_REGISTRY_URL=https://synthos-www.onrender.com
ENV SYNTHOS_BOOTSTRAP_IMMUNE_NODE=true

RUN mkdir -p /data
EXPOSE 8080

CMD ["/usr/local/bin/synthosd"]
