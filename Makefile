.PHONY: fmt-check test vet race build

fmt-check:
	test -z "$$(gofmt -l nextendo-nex server)"

test:
	cd nextendo-nex && go test ./...
	cd server && go test ./...

vet:
	cd nextendo-nex && go vet ./...
	cd server && go vet ./...

race:
	cd nextendo-nex && go test -race ./...
	cd server && go test -race ./...

build:
	mkdir -p dist
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ../dist/mk8d-server .
