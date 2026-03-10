.PHONY: help build-client build-server build-all run-client run-server stop clean test

# Цвета для вывода
BLUE := \033[0;34m
GREEN := \033[0;32m
RED := \033[0;31m
NC := \033[0m

help: ## Показать помощь
	@echo "$(BLUE)Tunnel Proxy - Makefile Commands$(NC)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "$(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

# Сборка
build-client: ## Собрать Docker образ клиента
	@echo "$(BLUE)Building client Docker image...$(NC)"
	docker build -t tunnel-client:latest -f client/Dockerfile .

build-server: ## Собрать Docker образ сервера
	@echo "$(BLUE)Building server Docker image...$(NC)"
	docker build -t tunnel-server:latest -f server/Dockerfile .

build-all: build-client build-server ## Собрать оба образа

# Запуск
run-client: ## Запустить клиент
	@echo "$(BLUE)Starting client...$(NC)"
	cd client && docker-compose up -d
	@echo "$(GREEN)Client started!$(NC)"
	@echo "Web UI: http://localhost:8080"
	@echo "SOCKS5: localhost:1080"

run-server: ## Запустить сервер (для тестирования локально)
	@echo "$(BLUE)Starting server...$(NC)"
	docker run -d \
		--name tunnel-server-local \
		--network host \
		--cap-add NET_ADMIN \
		-v $(PWD)/server/data:/app/data \
		tunnel-server:latest

stop: ## Остановить все контейнеры
	@echo "$(BLUE)Stopping containers...$(NC)"
	cd client && docker-compose down
	docker stop tunnel-server-local 2>/dev/null || true
	docker rm tunnel-server-local 2>/dev/null || true

# Логи
logs-client: ## Показать логи клиента
	cd client && docker-compose logs -f tunnel-client

logs-server: ## Показать логи сервера (локального)
	docker logs -f tunnel-server-local

# Разработка
dev-client: ## Запустить клиент в dev режиме
	cd client && go run cmd/client/main.go --config configs/client.yml

dev-server: ## Запустить сервер в dev режиме
	cd server && go run cmd/server/main.go --config configs/server.yml

# Тестирование
test: ## Запустить тесты
	@echo "$(BLUE)Running tests...$(NC)"
	cd shared && go test -v ./...
	cd client && go test -v ./...
	cd server && go test -v ./...

# Деплой
deploy-server: ## Деплоить сервер на VPS (использование: make deploy-server HOST=1.2.3.4 USER=root PASS=xxx)
	@if [ -z "$(HOST)" ]; then \
		echo "$(RED)Error: HOST not set$(NC)"; \
		echo "Usage: make deploy-server HOST=1.2.3.4 USER=root PASS=yourpass"; \
		exit 1; \
	fi
	@echo "$(BLUE)Deploying server to $(HOST)...$(NC)"
	./deploy/scripts/deploy-server.sh $(HOST) $(USER) $(PASS)

# Очистка
clean: ## Удалить все контейнеры и образы
	@echo "$(BLUE)Cleaning up...$(NC)"
	cd client && docker-compose down -v
	docker stop tunnel-server-local 2>/dev/null || true
	docker rm tunnel-server-local 2>/dev/null || true
	docker rmi tunnel-client:latest 2>/dev/null || true
	docker rmi tunnel-server:latest 2>/dev/null || true

# Обновление
update: ## Обновить и перезапустить клиент
	@echo "$(BLUE)Updating client...$(NC)"
	cd client && docker-compose pull
	cd client && docker-compose up -d
	@echo "$(GREEN)Client updated!$(NC)"

# Статус
status: ## Показать статус контейнеров
	@echo "$(BLUE)Container Status:$(NC)"
	@docker ps -a | grep -E "tunnel-client|tunnel-server" || echo "No tunnel containers running"

# Установка зависимостей для разработки
install-deps: ## Установить Go зависимости
	@echo "$(BLUE)Installing Go dependencies...$(NC)"
	cd shared && go mod download
	cd client && go mod download
	cd server && go mod download

# Форматирование кода
fmt: ## Форматировать Go код
	@echo "$(BLUE)Formatting code...$(NC)"
	cd shared && go fmt ./...
	cd client && go fmt ./...
	cd server && go fmt ./...

# Линтинг
lint: ## Запустить линтер (требуется golangci-lint)
	@echo "$(BLUE)Running linter...$(NC)"
	cd shared && golangci-lint run
	cd client && golangci-lint run
	cd server && golangci-lint run

# Создание архива для релиза
release: ## Создать архив релиза
	@echo "$(BLUE)Creating release archive...$(NC)"
	tar -czf tunnel-project-release.tar.gz \
		--exclude='.git' \
		--exclude='data' \
		--exclude='logs' \
		--exclude='*.db' \
		--exclude='.idea' \
		--exclude='.vscode' \
		.
	@echo "$(GREEN)Release archive created: tunnel-project-release.tar.gz$(NC)"

# Инициализация проекта
init: ## Первоначальная инициализация проекта
	@echo "$(BLUE)Initializing project...$(NC)"
	mkdir -p client/data client/logs
	mkdir -p server/data server/logs
	chmod +x deploy/scripts/*.sh
	@echo "$(GREEN)Project initialized!$(NC)"
	@echo "Next steps:"
	@echo "  1. make build-all"
	@echo "  2. make run-client"
	@echo "  3. Open http://localhost:8080"
