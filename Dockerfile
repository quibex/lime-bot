# Dockerfile для lime-bot
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Включаем CGO для работы с go-sqlite3
ENV CGO_ENABLED=1

RUN go build -tags "libsqlite3" -o lime-bot ./cmd/bot-service

FROM alpine:latest
RUN apk add --no-cache ca-certificates sqlite
COPY --from=builder /app/lime-bot /usr/local/bin/lime-bot
WORKDIR /data
VOLUME ["/data"]
ENV DB_DSN=file://data/limevpn.db
CMD ["/usr/local/bin/lime-bot"]
