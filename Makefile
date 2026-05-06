ifeq ($(OS),Windows_NT)
  EXT := .exe
else
  EXT :=
endif

.PHONY: build install test prompts mcp test-mcp test-frontend

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

test-mcp:
	cd mcp && go test ./...

test-frontend:
	npm test

test: test-frontend test-mcp
