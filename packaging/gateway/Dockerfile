# syntax=docker/dockerfile:1
# Official mailbox gateway image. Build from the repository root:
#   docker build -f packaging/gateway/Dockerfile -t xbot-gateway .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/xbot ./cmd/xbot

# No apk: some hosts cannot reach the Alpine CDN. BusyBox provides wget + adduser.
FROM alpine:3.22
RUN adduser -D -H -u 10001 xbot \
 && mkdir -p /data && chown xbot:xbot /data
COPY --from=build /out/xbot /usr/local/bin/xbot
USER xbot
VOLUME ["/data"]
EXPOSE 8790
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8790/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/xbot", "gateway", "serve"]
# --open: registration is open like the Worker; per-IP daily new-install cap
# and XBOT_GATEWAY_MAX_INSTALLS bound it. --behind-proxy: OpenResty in front.
CMD ["--open", "--behind-proxy", "--addr", ":8790", "--dir", "/data"]
