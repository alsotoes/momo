SHELL := /bin/bash
GO ?= go

BIN_DIR := bin
DOCS_DIR := docs
HTML_DOCS := $(DOCS_DIR)/html
SRC := $(shell find src -name '*.go')
BIN := $(BIN_DIR)/momo
MAIN := src/momo.go
MODULES := ./src/common ./src/crypto ./src/transport ./src/client ./src/metrics ./src/p2p ./src/server ./src/storage ./src/momofs

.PHONY: all build clean tidy vendor test vet coverage doc doc-live benchmark test-e2e smoke-tcp smoke-quic smoke-encryption-tcp smoke-encryption-quic smoke-encryption-s3-tcp smoke-encryption-s3-quic smoke-scale-cas test-contract test-load test-stress test-chaos test-metrics test-external-client blog-check monitoring-up monitoring-down pentest

all: build

build: $(BIN)

$(BIN): $(SRC)
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) $(MAIN)

doc:
	@mkdir -p $(HTML_DOCS)
	godoc -http=:6060 & \
	while ! nc -z localhost 6060; do sleep 1; done; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/ > $(HTML_DOCS)/index.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/client/ > $(HTML_DOCS)/client.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/common/ > $(HTML_DOCS)/common.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/crypto/ > $(HTML_DOCS)/crypto.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/metrics/ > $(HTML_DOCS)/metrics.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/momofs/ > $(HTML_DOCS)/momofs.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/p2p/ > $(HTML_DOCS)/p2p.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/server/ > $(HTML_DOCS)/server.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/storage/ > $(HTML_DOCS)/storage.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/src/transport/ > $(HTML_DOCS)/transport.html; \
	pkill godoc

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) work sync

vendor:
	$(GO) work vendor

clean:
	rm -rf $(BIN_DIR)
	rm -rf $(HTML_DOCS)
	rm -f coverage.out

test: vet
	CGO_ENABLED=1 $(GO) test -v -race -cover $(MODULES)

vet:
	$(GO) vet $(MODULES)

coverage:
	CGO_ENABLED=1 $(GO) test -race -coverprofile=coverage.out $(MODULES)
	$(GO) tool cover -html=coverage.out

benchmark:
	$(GO) test -run=^$$ -bench=. -benchmem -count=$(if $(COUNT),$(COUNT),1) $(MODULES)

test-e2e:
	./.github/scripts/test-e2e.sh

test-e2e-p2p:
	./.github/scripts/test-e2e-p2p.sh

smoke-tcp:
	./.github/scripts/test-e2e.sh momo-tcp

smoke-quic:
	./.github/scripts/test-e2e.sh momo-quic

smoke-s3-tcp:
	./.github/scripts/test-e2e.sh s3-tcp

smoke-s3-quic:
	./.github/scripts/test-e2e.sh s3-quic

smoke-encryption-tcp:
	./.github/scripts/test-e2e-encryption.sh momo-tcp

smoke-encryption-quic:
	./.github/scripts/test-e2e-encryption.sh momo-quic

smoke-encryption-s3-tcp:
	./.github/scripts/test-e2e-encryption.sh s3-tcp

smoke-encryption-s3-quic:
	./.github/scripts/test-e2e-encryption.sh s3-quic

test-external-client:
	./.github/scripts/test-external-client.sh

smoke-scale-cas:
	./.github/scripts/test-scale-cas.sh

install-hooks:
	@echo "Installing Git pre-commit hook..."
	@cp hooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Git hooks installed successfully!"

test-contract:
	$(GO) test -run TestContract -v ./src/server/...

test-load:
	@echo "Running k6 load test (requires momo server running on localhost:3333)..."
	k6 run tests/k6/load_test.js

test-stress:
	@echo "Running k6 stress test (requires momo server running on localhost:3333)..."
	k6 run tests/k6/stress_test.js

test-chaos:
	@echo "Running k6 chaos test (requires 3-node momo cluster running)..."
	k6 run tests/k6/chaos_test.js

test-metrics:
	./.github/scripts/test-metrics.sh

monitoring-up:
	@echo "Starting Grafana/Prometheus monitoring stack..."
	docker compose -f tests/monitoring/docker-compose-monitoring.yml up -d

monitoring-down:
	@echo "Stopping Grafana/Prometheus monitoring stack..."
	docker compose -f tests/monitoring/docker-compose-monitoring.yml down

pentest:
	@echo "Running DotDotPwn pentest (requires Perl + DotDotPwn cloned at /tmp/dotdotpwn)..."
	./.github/scripts/run-pentest.sh

blog-check:
	@echo "Validating docs/blog posts (Rule 76)..."
	python3 .github/scripts/blog_check.py
