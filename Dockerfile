# Dockerfile для lime-bot
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o lime-bot ./cmd/bot-service

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/lime-bot /usr/local/bin/lime-bot
WORKDIR /data
VOLUME ["/data"]
ENV DB_DSN=file://data/limevpn.db
CMD ["/usr/local/bin/lime-bot"]
