SHELL := /bin/bash
GO ?= go

BIN_DIR := bin
BIN := $(BIN_DIR)/momo
MAIN := src/momo.go

.PHONY: all build clean tidy vendor run-server run-client server0 server1 server2

all: build

build: $(BIN)

$(BIN):
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) $(MAIN)

tidy:
	$(GO) mod tidy

vendor:
	$(GO) mod vendor

clean:
	rm -rf $(BIN_DIR)

# Usage: make run-server ID=0
run-server:
	$(GO) run $(MAIN) -imp server -id $(ID)

server0:
	$(GO) run $(MAIN) -imp server -id 0

server1:
	$(GO) run $(MAIN) -imp server -id 1

server2:
	$(GO) run $(MAIN) -imp server -id 2

# Usage: make run-client FILE=/path/to/file
run-client:
	$(GO) run $(MAIN) -imp client -file $(FILE)

