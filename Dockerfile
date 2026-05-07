FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /mc-api ./cmd/mc-api

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /mc-api /mc-api

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/mc-api"]
