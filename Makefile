.PHONY: test build vet

test:
	go test ./...

build:
	go build -o bin/piramid ./cmd/piramid

vet:
	go vet ./...
