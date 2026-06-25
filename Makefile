.PHONY: test e2e build vet verify

test:
	go test ./...

e2e:
	go test ./test/e2e -count=1

build:
	go build -o bin/piramid ./cmd/piramid

vet:
	go vet ./...

verify:
	go test ./... -race
	go vet ./...
	CGO_ENABLED=0 go build -trimpath -o bin/piramid ./cmd/piramid
