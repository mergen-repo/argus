# ── Stage 1: Build operator-sim binary ──
FROM golang:1.25-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447 AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/operator-sim \
    ./cmd/operator-sim

# ── Stage 2: Minimal runtime ──
FROM alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app
COPY --from=builder /app/operator-sim /app/operator-sim

EXPOSE 9595 9596
ENTRYPOINT ["/app/operator-sim"]
