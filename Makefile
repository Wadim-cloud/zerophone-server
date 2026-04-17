.PHONY: build run test clean deps install

BINARY_NAME=zerophone
DB_PATH=zerophone.db
PORT=8080

build:
	go build -o $(BINARY_NAME) ./cmd/zerophone

run: build
	./$(BINARY_NAME) --db $(DB_PATH) --addr :$(PORT)

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME)
	rm -f $(DB_PATH)

deps:
	go mod download
	go mod tidy

install:
	go install ./...

# Development with auto-reload (requires air)
dev:
	air

# Generate container image
docker-build:
	docker build -t $(BINARY_NAME):latest .

docker-run:
	docker run -p $(PORT):8080 -v $(PWD)/$(DB_PATH):/app/$(DB_PATH) $(BINARY_NAME):latest

# Initialize database schema
init-db:
	@echo "Database will be created automatically on first run"
