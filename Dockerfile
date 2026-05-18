ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN set -eux; \
    target_os="${TARGETOS:-linux}"; \
    target_arch="${TARGETARCH:-amd64}"; \
    if [ "$target_arch" = "arm" ]; then \
      export GOARM="${TARGETVARIANT#v}"; \
      if [ -z "$GOARM" ]; then export GOARM=7; fi; \
    fi; \
    mkdir -p /out; \
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
      go build -trimpath -ldflags="-s -w" -o /out/ecs-controller ./cmd/ecs-controller

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /out/ecs-controller ./ecs-controller
COPY web ./web

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/ecs-controller"]
