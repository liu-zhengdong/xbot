# syntax=docker/dockerfile:1
# Account API image. Build from the repository root:
#   docker build -f packaging/account-api/Dockerfile -t xbot-account .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/xbot ./cmd/xbot

# No apk: some hosts cannot reach the Alpine CDN. BusyBox provides wget + adduser.
FROM alpine:3.22
RUN adduser -D -H -u 10001 account \
 && mkdir -p /data && chown account:account /data
COPY --from=build /out/xbot /usr/local/bin/xbot
USER account
# HOME is /tmp: the launcher touches the home directory before it dispatches to
# `account-api`, and the service user has none.
ENV HOME=/tmp DATA_DIR=/data ADDR=:8790 MAIL_MODE=outbox
VOLUME ["/data"]
EXPOSE 8790
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8790/account/v1/health || exit 1
ENTRYPOINT ["/usr/local/bin/xbot", "account-api"]
