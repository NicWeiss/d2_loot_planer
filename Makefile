SHELL=/bin/sh

VERSION ?= $(shell cat VERSION)
REGISTRY ?= harbor.nic-weiss.tech
PROJECT ?= d2_bt_analytics
IMAGE ?= $(REGISTRY)/$(PROJECT)/app

SSH_HOST ?= myvps
REMOTE_DIR ?= /root/projects/d2_bt_analytics

export VERSION IMAGE

.PHONY: help version dev start stop logs test build publish deploy release

help:  ## Помощь
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

version:  ## Версия проекта (из файла VERSION)
	@echo $(VERSION)

dev:  ## Разработка без Docker: http://localhost:8765, фронт читается с диска
	MARKET_REFRESH_HOURS=0 FRONTEND_DIR=frontend PORT=8765 go run ./cmd/server

start:  ## Запуск проекта локально: http://localhost:8000
	docker compose up -d --build
	@echo "http://localhost:8000"

stop:  ## Остановка локального проекта
	docker compose down

logs:  ## Лог локального контейнера
	docker compose logs -f --tail=100

test:  ## Тесты
	go vet ./...
	go test ./...

build:  ## Сборка образа: тэги <версия> и latest
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

publish:  ## Выкладка образа в harbor.nic-weiss.tech
	@docker image inspect $(IMAGE):$(VERSION) >/dev/null 2>&1 || { echo "нет образа $(IMAGE):$(VERSION), сначала make build"; exit 1; }
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

deploy:  ## Развёртывание на myvps в /root/projects/d2_bt_analytics (только свой контейнер)
	ssh $(SSH_HOST) 'mkdir -p $(REMOTE_DIR)'
	scp deploy/docker-compose.yml deploy/Makefile $(SSH_HOST):$(REMOTE_DIR)/
	ssh $(SSH_HOST) 'cd $(REMOTE_DIR) && make update VERSION=$(VERSION)'

release: test build publish deploy  ## Тесты, сборка, выкладка и развёртывание одной командой
