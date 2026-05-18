FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN mkdir -p /out && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/ecs-controller ./cmd/ecs-controller

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/ecs-controller ./ecs-controller
COPY web ./web

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/ecs-controller"]
