FROM node:20-alpine AS dashboard
WORKDIR /src/web/dashboard
COPY web/dashboard/package.json web/dashboard/package-lock.json* ./
RUN npm ci
COPY web/dashboard/ ./
RUN npm run build

FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git build-base

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=dashboard /src/internal/web/dashboard ./internal/web/dashboard
RUN CGO_ENABLED=0 go build -o /debridnest ./cmd/debridnest

FROM alpine:3.20

ARG WITH_FFMPEG=0
RUN apk add --no-cache ca-certificates \
    && if [ "$WITH_FFMPEG" = "1" ]; then apk add --no-cache ffmpeg; fi

WORKDIR /app
COPY --from=builder /debridnest /usr/local/bin/debridnest

ENV DEBRIDNEST_DATA_DIR=/data
ENV DEBRIDNEST_LISTEN=:8080

EXPOSE 8080 42069/tcp 42069/udp

VOLUME ["/data"]

ENTRYPOINT ["debridnest"]
