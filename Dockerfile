# syntax=docker/dockerfile:1.7

# ---------- Stage 1: builder ----------
FROM golang:1.25-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG TARGET=mc-api

RUN apk add --no-cache git ca-certificates

WORKDIR /build

COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/app ./cmd/${TARGET}

# ---------- Stage 2: mc-api ----------
FROM gcr.io/distroless/static-debian12:nonroot AS mc-api

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]

# ---------- Stage 2: wc-sync ----------
FROM gcr.io/distroless/static-debian12:nonroot AS wc-sync

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 8081
ENTRYPOINT ["/app"]

# ---------- Stage 2: content-worker ----------
FROM gcr.io/distroless/static-debian12:nonroot AS content-worker

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/app"]

# ---------- Stage 2: agent-worker ----------
FROM gcr.io/distroless/static-debian12:nonroot AS agent-worker

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/app"]

# ---------- Stage 2: temporal-worker ----------
FROM gcr.io/distroless/static-debian12:nonroot AS temporal-worker

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/app"]

# ---------- Stage 2: uiauto-compare ----------
FROM gcr.io/distroless/static-debian12:nonroot AS uiauto-compare

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/app"]

# ---------- Stage 2: ec-cli ----------
FROM gcr.io/distroless/static-debian12:nonroot AS ec-cli

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
ENTRYPOINT ["/app"]

# ---------- Stage 2: evomap-rollup ----------
FROM gcr.io/distroless/static-debian12:nonroot AS evomap-rollup

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 9090
ENTRYPOINT ["/app"]

# ---------- Default target (backward compat) ----------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/app /app

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app"]
