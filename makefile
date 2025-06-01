.PHONY: run test build clean test-integration

# Variables
DOCKER_COMPOSE_FILE := docker/docker-compose.yml
APP_NAME := imgScale
PORT := 8081
STORAGE_TYPE := memory
TEST_NGINX_PORT := 8082  # Используем другой порт для тестов

run: build
	@echo "Starting nginx with images..."
	@docker-compose -f $(DOCKER_COMPOSE_FILE) up -d nginx
	@echo "Running server with PORT=$(PORT) and STORAGE_TYPE=$(STORAGE_TYPE)..."
	@PORT=$(PORT) STORAGE_TYPE=$(STORAGE_TYPE) ./$(APP_NAME)

test: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	@go test ./...

test-integration: build-nginx
	@echo "Starting integration tests with nginx on port $(TEST_NGINX_PORT)..."
	@TEST_NGINX_PORT=$(TEST_NGINX_PORT) go test -v ./server/... -run TestIntegration

build:
	@echo "Building application..."
	@go build -o $(APP_NAME) ./server

build-nginx:
	@echo "Building Nginx image..."
	@docker build -t my-nginx-image -f docker/Dockerfile docker/

clean:
	@echo "Cleaning up..."
	@docker-compose -f $(DOCKER_COMPOSE_FILE) down
	@rm -f $(APP_NAME)
	@docker rm -f test-nginx 2>/dev/null || true