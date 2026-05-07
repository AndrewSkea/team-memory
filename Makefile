ifeq ($(OS),Windows_NT)
  EXT := .exe
else
  EXT :=
endif

.PHONY: build install test prompts mcp test-mcp test-frontend test-e2e test-all

prompts:
	bash scripts/copy-prompts.sh

build: prompts
	cd mcp && go build -o team-memory-mcp$(EXT) .

mcp: build

install: build
	mkdir -p ~/bin
	cp mcp/team-memory-mcp$(EXT) ~/bin/
	@echo "Installed to ~/bin/team-memory-mcp$(EXT)"
	@echo "Make sure ~/bin is on your PATH"
	@echo "To wire hooks and MCP, run: sh install.sh"

test-mcp: build
	cd mcp && go test ./...

test-frontend:
	npm test

test-e2e: build
	npm run test:e2e

test: test-frontend test-mcp

test-all: test test-e2e
