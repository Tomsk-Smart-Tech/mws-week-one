# ============================================================
# Multi-stage Dockerfile for crdt-engine
# Produces a minimal Alpine-based image (~15 MB).
# ============================================================

# ---------- Stage 1: Build ----------
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /bin/crdt-engine ./cmd/server

# ---------- Stage 2: Runtime ----------
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /bin/crdt-engine /usr/local/bin/crdt-engine

EXPOSE 8081

ENTRYPOINT ["crdt-engine"]
