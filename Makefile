dev:
	go run ./cmd/bot-service/main.go

run:
	go build -o bin/bot-service ./cmd/bot-service && ./bin/bot-service

migrate:
	go run ./cmd/bot-service/main.go migrate

tidy:
	go mod tidy

all: tidy
	go build -o bin/bot-service ./cmd/bot-service

# Сборка с CGO для SQLite (для локальной разработки)
build-cgo: tidy
	CGO_ENABLED=1 go build -o bin/bot-service ./cmd/bot-service

# Docker сборка
docker-build:
	docker build -t lime-bot .

generate:
	@echo "Generating protobuf files..."
	protoc --go_out=. --go-grpc_out=. pkg/wgagent/wgagent.proto

ssh:
	@echo "Connecting to production server..."
	ssh -i ~/.ssh/lime-bot-deploy root@77.246.102.133
