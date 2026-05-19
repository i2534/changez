BINARY  := changez
CMD     := ./cmd/changez
PIDFILE := .changez.pid
CONFIG  := config.yaml

.PHONY: build start stop check restart clean

build:
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

clean: stop
	rm -f $(BINARY) $(PIDFILE)
