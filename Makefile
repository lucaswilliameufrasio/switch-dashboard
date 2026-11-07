SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

BINARY_NAME := switch-dashboard
FRONTEND_DIR := frontend
DIST_DIR := web/dist
HOST ?= 127.0.0.1
PORT ?= 8080

# Detecta automaticamente o mise para usar as versões definidas no mise.toml
MISE := $(shell command -v mise 2>/dev/null)
ifdef MISE
	RUN := mise exec --
else
	RUN :=
endif

GO := $(RUN) go
NPM := $(RUN) npm --prefix $(FRONTEND_DIR)

.PHONY: help all setup frontend backend build run run-vpn dev dev-frontend dev-backend test vet tidy clean docker-build docker-up docker-down docker-logs

help: ## Exibe esta mensagem de ajuda com todos os alvos disponíveis
	@echo "======================================================================"
	@echo "  UniFi Switch OS (Go + SvelteKit) - Makefile"
	@echo "======================================================================"
	@awk 'BEGIN {FS = ":.*##"; printf "\nUso:\n  make \033[36m<alvo>\033[0m\n\nAlvos:\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""
	@echo "Exemplos com Tailscale / NetBird:"
	@echo "  [Fish] make run HOST=(tailscale ip -4) PORT=8080"
	@echo "  [Bash] make run HOST=\$$(tailscale ip -4) PORT=8080"
	@echo "  [Fish] make run HOST=(netbird status | awk '/NetBird IP:/ {split(\$$3,a,\"/\"); print a[1]}') PORT=8080"
	@echo "  [Bash] make run HOST=\$$(netbird status | awk '/NetBird IP:/ {split(\$$3,a,\"/\"); print a[1]}') PORT=8080"
	@echo ""

all: build ## Compila o frontend SvelteKit e o binário Go completo

setup: ## Instala ferramentas do mise, dependências NPM e módulos Go
	@if [ -n "$(MISE)" ]; then mise trust && mise install; fi
	$(NPM) install
	$(GO) mod download
	$(GO) mod tidy

frontend: ## Compila o frontend SvelteKit estático em web/dist
	$(NPM) run build

backend: ## Compila o binário Go embutindo os arquivos de web/dist
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/server

build: frontend backend ## Build completo de produção (SvelteKit SPA + Binário Go único)
	@echo "Build concluído: ./$(BINARY_NAME)"

run: build ## Compila e executa o servidor em modo produção (127.0.0.1:8080)
	DASHBOARD_HOST=$(HOST) DASHBOARD_PORT=$(PORT) ./$(BINARY_NAME)

run-vpn: build ## Detecta o IP da interface Tailscale (tailscale0) ou NetBird (wt0) e escuta apenas na VPN
	@VPN_IP=$$(ip -4 -o addr show dev tailscale0 2>/dev/null | awk '{split($$4,a,"/"); print a[1]}' | head -n1); \
	if [ -z "$$VPN_IP" ]; then \
		VPN_IP=$$(ip -4 -o addr show dev wt0 2>/dev/null | awk '{split($$4,a,"/"); print a[1]}' | head -n1); \
	fi; \
	if [ -z "$$VPN_IP" ]; then \
		echo "Erro: Nenhuma interface tailscale0 ou wt0 ativa encontrada."; exit 1; \
	fi; \
	echo "Escutando exclusivamente na VPN Mesh em http://$$VPN_IP:$(PORT)"; \
	DASHBOARD_HOST="$$VPN_IP" DASHBOARD_PORT=$(PORT) ./$(BINARY_NAME)

dev-backend: ## Executa apenas o backend Go diretamente (sem recompilar o frontend)
	DASHBOARD_HOST=$(HOST) DASHBOARD_PORT=$(PORT) $(GO) run ./cmd/server

dev-frontend: ## Inicia o servidor Vite do SvelteKit em modo desenvolvimento (hot-reload)
	$(NPM) run dev

test: ## Executa a suíte de testes unitários do Go com detector de race conditions
	$(GO) test -v -race ./...

vet: ## Executa análise estática (go vet) no código Go
	$(GO) vet ./...

tidy: ## Limpa e atualiza o go.mod e go.sum
	$(GO) mod tidy

docker-build: ## Constrói a imagem Docker multi-stage endurecida (non-root)
	docker compose build

docker-up: ## Sobe o container em background (escutando em 127.0.0.1:8080)
	docker compose up -d --build

docker-down: ## Para e remove o container Docker
	docker compose down

docker-logs: ## Exibe os logs em tempo real do container Docker
	docker compose logs -f

clean: ## Remove o binário compilado e os artefatos de build do frontend
	rm -f $(BINARY_NAME)
	rm -rf $(FRONTEND_DIR)/.svelte-kit $(FRONTEND_DIR)/build
	find $(DIST_DIR) -mindepth 1 ! -name 'index.html' ! -name '.gitkeep' -exec rm -rf {} +
