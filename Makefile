# Static linting of source files. See .golangci.yaml for options.
lint:
	golangci-lint run

generate:
	go generate ./...

# Run tests with race detector, measure coverage.
test:
	go test -race -covermode=atomic -coverpkg=./... -coverprofile=cover.out ./interp
	
# Open coverage info in browser
cover: test
	go tool cover -html=cover.out
