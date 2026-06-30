FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o /app/bin/server \
    ./cmd/server/

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata wget

ENV TZ=UTC

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=builder /app/bin/server /app/server

USER app

EXPOSE 5006

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:5006/healthz || exit 1

ENTRYPOINT ["/app/server"]
