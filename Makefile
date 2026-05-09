.PHONY: up down test migrate load seed fmt vet build

up:
	docker compose up --build -d

down:
	docker compose down

down-volumes:
	docker compose down -v

test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

build:
	go build -o bin/streamvault ./cmd/server

fmt:
	gofmt -w .

vet:
	go vet ./...

migrate:
	docker compose run --rm migrate

load:
	go run scripts/loadtest.go

seed:
	DATABASE_URL=postgres://streamvault:streamvault@localhost:5432/streamvault?sslmode=disable \
	go run scripts/seed.go

logs:
	docker compose logs -f app

psql:
	docker compose exec postgres psql -U streamvault -d streamvault
