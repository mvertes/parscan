# Static linting of source files. See .golangci.yaml for options.
lint:
	golangci-lint run

# Run tests with race detector, measure coverage.
test:
	go test -race -covermode=atomic -coverprofile=cover.out ./...
	
# Open coverage info in browser
cover: test
	go tool cover -html=cover.out
