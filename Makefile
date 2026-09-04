.PHONY: run build clean

run:
	go run cmd/bot/main.go

build:
	go build -o bin/sushi-bot cmd/bot/main.go

clean:
	rm -rf bin/

install:
	go mod download
	go mod tidy

dev:
	ENVIRONMENT=development LOG_LEVEL=debug go run cmd/bot/main.go