# bus-pooling · Makefile
#
# 常用命令。CI 只调 `make ci` 就够。

.PHONY: help build test test-race lint lint-terms vet ci e2e clean

help:
	@echo "targets:"
	@echo "  make build       编译 · 输出 ./bus-pooling"
	@echo "  make vet         go vet"
	@echo "  make lint-terms  扫源码里的内部术语泄漏（CLAUDE.md §0.1/§0.2/§7.3/§11/§12.6）"
	@echo "  make lint        所有 lint（含 vet + terms）"
	@echo "  make test        跑所有 Go 单测"
	@echo "  make test-race   race detector"
	@echo "  make ci          CI 用 · lint + vet + test"
	@echo "  make e2e         跑所有 e2e 脚本"

build:
	go build -o ./bus-pooling ./cmd/bus-pooling

vet:
	go vet ./...

lint-terms:
	python3 tools/lint/no_internal_terms.py

lint: vet lint-terms

test:
	go test -timeout 120s ./...

test-race:
	go test -race -timeout 180s ./...

ci: lint test

e2e:
	@for s in run-e2e sprint1a-flows multi-vendor topup-channels pending-topup anon-bus real-crash-window p0-regressions; do \
		echo "=== $$s ==="; \
		bash tests/e2e/$$s.sh || exit 1; \
	done

clean:
	rm -f ./bus-pooling
	rm -f /tmp/bp-*.db /tmp/bp-*-cookies.txt
