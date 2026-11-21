SHELL=/bin/sh

# Runs on the server in /root/projects/d2_bt_analytics. Touches only this compose project and its container.
COMPOSE = docker-compose -p d2_bt_analytics -f docker-compose.yml
CONTAINER = prod_d2_bt_analytics
VERSION ?= $(shell cat .version 2>/dev/null)

export VERSION

help:  ## Помощь
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

version:  ## Развёрнутая версия
	@echo $(VERSION)

update:  ## Скачать образ VERSION и перезапустить контейнер
	@test -n "$(VERSION)" || { echo "укажи VERSION=x.y.z"; exit 1; }
	mkdir -p data && chown 10001 data  # the app runs as uid 10001 inside the image
	touch .env
	$(COMPOSE) pull app
	# docker-compose 1.x cannot recreate containers on a new Docker Engine (KeyError: 'ContainerConfig'),
	# so this project's own app container is removed first and created fresh
	$(COMPOSE) rm -f -s app
	$(COMPOSE) up -d --no-deps app
	echo $(VERSION) > .version
	@for i in $$(seq 1 45); do \
	  s=$$(docker inspect -f '{{.State.Health.Status}}' $(CONTAINER) 2>/dev/null); \
	  [ "$$s" = healthy ] && { echo "$(CONTAINER) $(VERSION): healthy"; exit 0; }; sleep 2; \
	done; echo "$(CONTAINER) не стал healthy за 90 секунд: make log"; exit 1

start:  ## Запуск
	$(COMPOSE) up -d --no-deps app

stop:  ## Остановка (только этот контейнер)
	$(COMPOSE) stop app

ps:  ## Состояние
	$(COMPOSE) ps

log:  ## Лог
	@docker logs -f --tail=100 $(CONTAINER)
