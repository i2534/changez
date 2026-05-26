DIST    := dist
BINARY  := $(DIST)/changez
CMD     := ./cmd/changez
PIDFILE := .changez.pid
CONFIG  := config.yaml

.PHONY: build start stop check restart clean build-web

build-web:
	cd web && npm install && npm run build

build: build-web
	@mkdir -p $(DIST)
	go build -o $(BINARY) $(CMD)

start: build
	@if [ -f $(PIDFILE) ] && kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		echo "already running (pid $$(cat $(PIDFILE)))"; \
		else \
		nohup ./$(BINARY) -c $(CONFIG) >> changez.log 2>&1 & echo $$! > $(PIDFILE); \
		sleep 1; \
		echo "started (pid $$(cat $(PIDFILE)))"; \
	fi

stop:
	@if [ -f $(PIDFILE) ] && kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		kill $$(cat $(PIDFILE)) && echo "stopped (pid $$(cat $(PIDFILE)))" && rm -f $(PIDFILE); \
	else \
		echo "not running"; \
		rm -f $(PIDFILE); \
	fi

check:
	@if [ -f $(PIDFILE) ] && kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		echo "running (pid $$(cat $(PIDFILE)))"; \
		curl -sf http://127.0.0.1:8760/api/stats > /dev/null 2>&1 && echo "  ✓ HTTP OK" || echo "  ✗ HTTP unreachable"; \
	else \
		echo "not running"; \
	fi

restart: stop start

# Quick restart without rebuild (frontend-only change)
restart-web: build-web
	kill $(shell cat $(PIDFILE)) 2>/dev/null; rm -f $(PIDFILE); make start

# Development: run without installing (skips binary copy)
dev:
	go run $(CMD)

clean: stop
	rm -rf $(DIST)
	rm -f $(PIDFILE)
