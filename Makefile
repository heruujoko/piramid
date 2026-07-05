.PHONY: test e2e build vet verify web-build

test:
	go test ./...

e2e:
	go test ./test/e2e -count=1

web-build:
	cd web && npx vite build

build: web-build
	go build -o bin/piramid ./cmd/piramid

vet:
	go vet ./...

verify:
	go test ./... -race
	go vet ./...
	CGO_ENABLED=0 go build -trimpath -o bin/piramid ./cmd/piramid
