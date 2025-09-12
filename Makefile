SHELL := /bin/bash
GO ?= go

BIN_DIR := bin
DOCS_DIR := doc
HTML_DOCS := $(DOCS_DIR)/html
SRC := $(shell find src -name '*.go')
BIN := $(BIN_DIR)/momo
MAIN := src/momo.go
MODULES := ./src/common ./src/metrics ./src/server

.PHONY: all build clean tidy vendor test vet coverage doc doc-live

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
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/common/ > $(HTML_DOCS)/common.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/metrics/ > $(HTML_DOCS)/metrics.html; \
	curl -s http://localhost:6060/pkg/github.com/alsotoes/momo/server/ > $(HTML_DOCS)/server.html; \
	pkill godoc

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
