.PHONY: prompts mcp test-mcp test-frontend test

prompts:
	bash scripts/copy-prompts.sh

mcp: prompts
	cd mcp && go build -o team-memory-mcp .

test-mcp:
	cd mcp && go test ./...

test-frontend:
	npm test

test: test-frontend test-mcp
