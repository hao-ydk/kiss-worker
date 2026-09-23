# syntax=docker/dockerfile:1

FROM golang:alpine AS builder

LABEL stage=builder

ENV CGO_ENABLED 0
ENV GOOS linux
ENV GOPROXY https://goproxy.cn,direct

WORKDIR /build

COPY . .
RUN go mod download
RUN go build -ldflags="-s -w" -o app ./main.go



FROM alpine

WORKDIR /app
RUN apk add --no-cache su-exec \
    && addgroup -g 10001 -S kiss \
    && adduser -u 10001 -S -G kiss kiss \
    && mkdir -p /app/data \
    && chown -R kiss:kiss /app
COPY --from=builder --chown=kiss:kiss /build/app /app/
COPY --chmod=755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

ENTRYPOINT ["docker-entrypoint.sh"]
