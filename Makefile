# compile, format and test

build: format test

format:
	go fmt $(shell go list ./... | grep -v /vendor/) && \
	go vet $(shell go list ./... | grep -v /vendor/) && \
	golangci-lint run --fast --issues-exit-code 1

test:
	go test -race -covermode=atomic $(shell go list ./... | grep -v /vendor/)
	go test -bench=. $(shell go list ./... | grep -v /vendor/)
